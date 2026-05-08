package encoder

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

type Encoder struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr bytes.Buffer
	done   chan error
}

func New(ffmpegPath, output string, fps int, format string) (*Encoder, error) {
	if ffmpegPath == "" {
		return nil, fmt.Errorf("ffmpeg path is required")
	}
	if output == "" {
		return nil, fmt.Errorf("output path is required")
	}
	if fps <= 0 {
		return nil, fmt.Errorf("fps must be positive")
	}

	resolvedFormat, err := ResolveFormat(output, format)
	if err != nil {
		return nil, err
	}

	enc := &Encoder{done: make(chan error, 1)}
	args := buildFFmpegArgs(output, fps, resolvedFormat)
	enc.cmd = exec.Command(ffmpegPath, args...)
	enc.cmd.Stderr = &enc.stderr

	stdin, err := enc.cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open ffmpeg stdin: %w", err)
	}
	enc.stdin = stdin

	if err := enc.cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}
	go func() {
		err := enc.cmd.Wait()
		if err != nil {
			msg := strings.TrimSpace(enc.stderr.String())
			if msg != "" {
				err = fmt.Errorf("%w: %s", err, msg)
			}
		}
		enc.done <- err
	}()

	return enc, nil
}

func (e *Encoder) WriteFrame(png []byte) error {
	if len(png) == 0 {
		return fmt.Errorf("frame PNG is empty")
	}
	if _, err := e.stdin.Write(png); err != nil {
		return fmt.Errorf("write frame to ffmpeg: %w", err)
	}
	return nil
}

func (e *Encoder) Close() error {
	if e.stdin != nil {
		if err := e.stdin.Close(); err != nil {
			return fmt.Errorf("close ffmpeg stdin: %w", err)
		}
		e.stdin = nil
	}
	if err := <-e.done; err != nil {
		return fmt.Errorf("ffmpeg encode failed: %w", err)
	}
	return nil
}

func ResolveFormat(output, explicit string) (string, error) {
	format := strings.ToLower(strings.TrimSpace(explicit))
	if format == "" {
		switch strings.ToLower(filepath.Ext(output)) {
		case ".webm":
			format = "webm"
		default:
			format = "mp4"
		}
	}

	switch format {
	case "mp4", "webm":
		return format, nil
	default:
		return "", fmt.Errorf("unsupported format %q; expected mp4 or webm", explicit)
	}
}

func buildFFmpegArgs(output string, fps int, format string) []string {
	args := []string{
		"-f", "image2pipe",
		"-vcodec", "png",
		"-r", fmt.Sprintf("%d", fps),
		"-i", "pipe:0",
	}

	switch format {
	case "webm":
		args = append(args,
			"-vcodec", "libvpx-vp9",
			"-pix_fmt", "yuva420p",
			"-b:v", "0",
			"-crf", "30",
		)
	default:
		args = append(args,
			"-vcodec", "libx264",
			"-pix_fmt", "yuv420p",
			"-movflags", "+faststart",
		)
	}

	args = append(args, "-y", output)
	return args
}
