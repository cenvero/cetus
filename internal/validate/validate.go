package validate

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/cenvero/cetus/internal/compose"
	"golang.org/x/net/html"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

type Finding struct {
	Severity Severity
	Message  string
}

type Result struct {
	Composition *compose.Composition
	Findings    []Finding
}

func (r *Result) ErrorCount() int {
	return r.count(SeverityError)
}

func (r *Result) WarningCount() int {
	return r.count(SeverityWarning)
}

func (r *Result) count(severity Severity) int {
	if r == nil {
		return 0
	}
	total := 0
	for _, finding := range r.Findings {
		if finding.Severity == severity {
			total++
		}
	}
	return total
}

func Check(path string) (*Result, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("stat composition: %w", err)
	}

	doc, err := parseHTML(path)
	if err != nil {
		return nil, err
	}

	result := &Result{}
	composition, err := compose.Parse(path)
	if err != nil {
		result.add(SeverityError, err.Error())
	} else {
		result.Composition = composition
		if len(composition.Clips) == 0 {
			result.add(SeverityWarning, "no .clip elements found; the render may be blank unless custom frame hooks draw the scene")
		}
	}

	root := findCompositionRoot(doc)
	checkAssetReferences(result, doc, filepath.Dir(path))
	checkGSAP(result, doc)
	if root != nil && result.Composition != nil {
		checkInlineBounds(result, root, result.Composition)
		checkDuplicateClipIDs(result, root)
	}

	return result, nil
}

func (r *Result) add(severity Severity, message string) {
	r.Findings = append(r.Findings, Finding{Severity: severity, Message: message})
}

func parseHTML(path string) (*html.Node, error) {
	file, err := os.Open(path) // #nosec G304 -- validating a user-selected composition file is the purpose of this package.
	if err != nil {
		return nil, fmt.Errorf("open composition HTML: %w", err)
	}
	defer file.Close()

	doc, err := html.Parse(file)
	if err != nil {
		return nil, fmt.Errorf("parse composition HTML: %w", err)
	}
	return doc, nil
}

func findCompositionRoot(doc *html.Node) *html.Node {
	var found *html.Node
	walk(doc, func(n *html.Node) {
		if found == nil && n.Type == html.ElementNode && strings.TrimSpace(attr(n, "data-composition-id")) != "" {
			found = n
		}
	})
	return found
}

func checkDuplicateClipIDs(result *Result, root *html.Node) {
	seen := make(map[string]bool)
	walk(root, func(n *html.Node) {
		if n.Type != html.ElementNode || !hasClass(n, "clip") {
			return
		}
		id := strings.TrimSpace(attr(n, "id"))
		if id == "" {
			return
		}
		if seen[id] {
			result.add(SeverityWarning, fmt.Sprintf("duplicate clip id %q; use unique ids for stable animation targeting", id))
			return
		}
		seen[id] = true
	})
}

func checkInlineBounds(result *Result, root *html.Node, composition *compose.Composition) {
	walk(root, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		style := attr(n, "style")
		if style == "" {
			return
		}

		left, hasLeft := pxStyleValue(style, "left")
		top, hasTop := pxStyleValue(style, "top")
		width, hasWidth := pxStyleValue(style, "width")
		height, hasHeight := pxStyleValue(style, "height")
		label := nodeLabel(n)

		if hasLeft && left >= float64(composition.Width) {
			result.add(SeverityWarning, fmt.Sprintf("%s has inline left %.0fpx outside the %dpx frame", label, left, composition.Width))
		}
		if hasTop && top >= float64(composition.Height) {
			result.add(SeverityWarning, fmt.Sprintf("%s has inline top %.0fpx outside the %dpx frame", label, top, composition.Height))
		}
		if hasLeft && hasWidth && left+width <= 0 {
			result.add(SeverityWarning, fmt.Sprintf("%s has inline left+width outside the left edge of the frame", label))
		}
		if hasTop && hasHeight && top+height <= 0 {
			result.add(SeverityWarning, fmt.Sprintf("%s has inline top+height outside the top edge of the frame", label))
		}
	})
}

func pxStyleValue(style, property string) (float64, bool) {
	for _, decl := range strings.Split(style, ";") {
		name, value, ok := strings.Cut(decl, ":")
		if !ok || strings.TrimSpace(strings.ToLower(name)) != property {
			continue
		}
		value = strings.TrimSpace(strings.ToLower(value))
		value = strings.TrimSuffix(value, "!important")
		value = strings.TrimSpace(value)
		if !strings.HasSuffix(value, "px") {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(value, "px")), 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

var cssURLPattern = regexp.MustCompile(`url\(\s*['"]?([^'")]+)['"]?\s*\)`)

func checkAssetReferences(result *Result, doc *html.Node, baseDir string) {
	walk(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		for _, ref := range nodeReferences(n) {
			checkReference(result, baseDir, ref.value, fmt.Sprintf("%s %s", nodeLabel(n), ref.name))
		}
		if style := attr(n, "style"); style != "" {
			checkCSSURLs(result, baseDir, style, nodeLabel(n)+" style")
		}
		if n.Data == "style" {
			checkCSSURLs(result, baseDir, textContent(n), "<style>")
		}
	})
}

type reference struct {
	name  string
	value string
}

func nodeReferences(n *html.Node) []reference {
	switch n.Data {
	case "img":
		return []reference{{"src", attr(n, "src")}, {"srcset", attr(n, "srcset")}}
	case "video":
		return []reference{{"src", attr(n, "src")}, {"poster", attr(n, "poster")}}
	case "audio", "source", "track", "script":
		return []reference{{"src", attr(n, "src")}}
	case "link":
		return []reference{{"href", attr(n, "href")}}
	default:
		return nil
	}
}

func checkCSSURLs(result *Result, baseDir, css, label string) {
	for _, match := range cssURLPattern.FindAllStringSubmatch(css, -1) {
		if len(match) > 1 {
			checkReference(result, baseDir, match[1], label+" url()")
		}
	}
}

func checkReference(result *Result, baseDir, raw, label string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	if strings.Contains(label, "srcset") {
		for _, item := range strings.Split(raw, ",") {
			fields := strings.Fields(strings.TrimSpace(item))
			if len(fields) > 0 {
				checkReference(result, baseDir, fields[0], label)
			}
		}
		return
	}
	if shouldIgnoreReference(raw) {
		return
	}
	if strings.HasPrefix(raw, "//") {
		result.add(SeverityWarning, fmt.Sprintf("%s uses remote URL %q; prefer local assets for deterministic renders", label, raw))
		return
	}

	u, err := url.Parse(raw)
	if err != nil {
		result.add(SeverityError, fmt.Sprintf("%s has invalid URL %q: %v", label, raw, err))
		return
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		result.add(SeverityWarning, fmt.Sprintf("%s uses remote URL %q; prefer local assets for deterministic renders", label, raw))
		return
	case "data", "blob", "about":
		return
	case "file":
		checkLocalFile(result, u.Path, label, raw)
		return
	case "":
		path := raw
		if u.Path != "" {
			path = u.Path
		}
		if unescaped, err := url.PathUnescape(path); err == nil {
			path = unescaped
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		checkLocalFile(result, path, label, raw)
		return
	default:
		result.add(SeverityError, fmt.Sprintf("%s uses unsupported URL scheme %q in %q", label, u.Scheme, raw))
	}
}

func shouldIgnoreReference(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.HasPrefix(raw, "#") ||
		strings.HasPrefix(lower, "javascript:") ||
		strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(lower, "tel:")
}

func checkLocalFile(result *Result, path, label, raw string) {
	if path == "" {
		return
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		result.add(SeverityError, fmt.Sprintf("%s references missing local asset %q", label, raw))
		return
	}
	if err != nil {
		result.add(SeverityWarning, fmt.Sprintf("%s could not stat local asset %q: %v", label, raw, err))
		return
	}
	if info.IsDir() {
		result.add(SeverityError, fmt.Sprintf("%s references directory %q, expected a file", label, raw))
	}
}

var pausedTruePattern = regexp.MustCompile(`paused\s*:\s*true`)

func checkGSAP(result *Result, doc *html.Node) {
	script := scriptText(doc)
	if !strings.Contains(script, "gsap.timeline") {
		return
	}
	if !pausedTruePattern.MatchString(script) {
		result.add(SeverityError, "GSAP timeline is created without paused: true; Cetus cannot seek a running timeline deterministically")
	}
	if !strings.Contains(script, "__timelines.push") {
		result.add(SeverityError, "GSAP timeline is created but not registered with window.__timelines.push(tl)")
	}
}

func scriptText(doc *html.Node) string {
	var builder strings.Builder
	walk(doc, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "script" {
			builder.WriteString(textContent(n))
			builder.WriteString("\n")
		}
	})
	return builder.String()
}

func textContent(n *html.Node) string {
	var builder strings.Builder
	var collect func(*html.Node)
	collect = func(cur *html.Node) {
		if cur.Type == html.TextNode {
			builder.WriteString(cur.Data)
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			collect(child)
		}
	}
	collect(n)
	return builder.String()
}

func walk(n *html.Node, fn func(*html.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		walk(child, fn)
	}
}

func nodeLabel(n *html.Node) string {
	if n == nil {
		return "<unknown>"
	}
	if id := strings.TrimSpace(attr(n, "id")); id != "" {
		return fmt.Sprintf("<%s#%s>", n.Data, id)
	}
	if class := strings.TrimSpace(attr(n, "class")); class != "" {
		firstClass := strings.Fields(class)[0]
		return fmt.Sprintf("<%s.%s>", n.Data, firstClass)
	}
	return fmt.Sprintf("<%s>", n.Data)
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	for _, field := range strings.Fields(attr(n, "class")) {
		if field == class {
			return true
		}
	}
	return false
}
