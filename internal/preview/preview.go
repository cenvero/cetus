package preview

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
	xhtml "golang.org/x/net/html"
)

func Serve(htmlPath string, port int, noOpen bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return ServeContext(ctx, htmlPath, port, noOpen)
}

func ServeContext(ctx context.Context, htmlPath string, port int, noOpen bool) error {
	absHTML, err := filepath.Abs(htmlPath)
	if err != nil {
		return fmt.Errorf("resolve preview HTML path: %w", err)
	}
	if info, err := os.Stat(absHTML); err != nil {
		return fmt.Errorf("stat preview HTML: %w", err)
	} else if info.IsDir() {
		return fmt.Errorf("preview input must be a file")
	}

	listener, err := listen(port)
	if err != nil {
		return err
	}
	defer listener.Close()

	actualPort := listener.Addr().(*net.TCPAddr).Port
	hub := newHub()
	server := &http.Server{
		Handler: previewHandler(absHTML, actualPort, hub),
	}

	serverErr := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	watcher, err := startWatcher(absHTML, hub)
	if err != nil {
		_ = server.Shutdown(context.Background())
		return err
	}
	defer watcher.Close()

	url := fmt.Sprintf("http://localhost:%d/", actualPort)
	fmt.Printf("Preview serving %s at %s\n", absHTML, url)
	if !noOpen {
		if err := openBrowser(url); err != nil {
			log.Printf("open browser: %v", err)
		}
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown preview server: %w", err)
		}
		if err := <-serverErr; err != nil {
			return fmt.Errorf("preview server: %w", err)
		}
		return nil
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("preview server: %w", err)
		}
		return nil
	}
}

type hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

func newHub() *hub {
	return &hub{clients: make(map[*websocket.Conn]struct{})}
}

func (h *hub) add(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[conn] = struct{}{}
}

func (h *hub) remove(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, conn)
	_ = conn.Close()
}

func (h *hub) broadcast(message string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
			_ = conn.Close()
			delete(h.clients, conn)
		}
	}
}

func previewHandler(htmlPath string, port int, hub *hub) http.Handler {
	baseDir := filepath.Dir(htmlPath)
	fileName := filepath.Base(htmlPath)
	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/__cetus_reload", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		hub.add(conn)
		go func() {
			defer hub.remove(conn)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		cleanPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if r.URL.Path == "/" || cleanPath == "." || cleanPath == fileName {
			serveInjectedHTML(w, htmlPath, port)
			return
		}

		target := filepath.Clean(filepath.Join(baseDir, cleanPath))
		rel, err := filepath.Rel(baseDir, target)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		http.ServeFile(w, r, target)
	})

	return mux
}

func serveInjectedHTML(w http.ResponseWriter, htmlPath string, port int) {
	data, err := os.ReadFile(htmlPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	patched := injectReloadScript(data, port)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(patched)
}

func injectReloadScript(data []byte, port int) []byte {
	script := []byte(fmt.Sprintf(`<script>
(function() {
  var ws = new WebSocket('ws://localhost:%d/__cetus_reload');
  ws.onmessage = function(e) {
    if (e.data === 'reload') window.location.reload();
  };
  ws.onclose = function() {
    setTimeout(function() { window.location.reload(); }, 1000);
  };
})();
</script>`, port))

	lower := bytes.ToLower(data)
	if idx := bytes.LastIndex(lower, []byte("</body>")); idx >= 0 {
		out := make([]byte, 0, len(data)+len(script))
		out = append(out, data[:idx]...)
		out = append(out, script...)
		out = append(out, data[idx:]...)
		return out
	}

	out := make([]byte, 0, len(data)+len(script))
	out = append(out, data...)
	out = append(out, script...)
	return out
}

func startWatcher(htmlPath string, hub *hub) (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create file watcher: %w", err)
	}

	dirs := referencedAssetDirs(htmlPath)
	dirs[filepath.Dir(htmlPath)] = struct{}{}
	for dir := range dirs {
		if err := watcher.Add(dir); err != nil {
			_ = watcher.Close()
			return nil, fmt.Errorf("watch %s: %w", dir, err)
		}
	}

	go func() {
		timer := time.NewTimer(time.Hour)
		if !timer.Stop() {
			<-timer.C
		}
		pending := false
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
					continue
				}
				pending = true
				timer.Reset(100 * time.Millisecond)
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("preview watcher: %v", err)
			case <-timer.C:
				if pending {
					pending = false
					hub.broadcast("reload")
				}
			}
		}
	}()

	return watcher, nil
}

func referencedAssetDirs(htmlPath string) map[string]struct{} {
	dirs := make(map[string]struct{})
	data, err := os.ReadFile(htmlPath)
	if err != nil {
		return dirs
	}
	doc, err := xhtml.Parse(bytes.NewReader(data))
	if err != nil {
		return dirs
	}

	baseDir := filepath.Dir(htmlPath)
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			for _, attr := range n.Attr {
				if attr.Key != "src" && attr.Key != "href" && attr.Key != "poster" {
					continue
				}
				value := strings.TrimSpace(html.UnescapeString(attr.Val))
				if value == "" || strings.Contains(value, "://") || strings.HasPrefix(value, "data:") || strings.HasPrefix(value, "#") {
					continue
				}
				path := filepath.Join(baseDir, filepath.FromSlash(value))
				if info, err := os.Stat(path); err == nil {
					if info.IsDir() {
						dirs[path] = struct{}{}
					} else {
						dirs[filepath.Dir(path)] = struct{}{}
					}
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return dirs
}

func listen(port int) (net.Listener, error) {
	addr := "127.0.0.1:0"
	if port > 0 {
		addr = fmt.Sprintf("127.0.0.1:%d", port)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("start preview listener: %w", err)
	}
	return listener, nil
}

func openBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch browser: %w", err)
	}
	return nil
}
