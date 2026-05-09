package encoder

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Options struct {
	AudioPath           string
	AudioVolume         float64
	AudioVolumeSet      bool
	AudioLoop           bool
	AudioStartSeconds   float64
	AudioFadeInSeconds  float64
	AudioFadeOutSeconds float64
	DurationSeconds     float64
}

type Encoder struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr bytes.Buffer
	done   chan error
}

func New(ffmpegPath, output string, fps int, format string, opts Options) (*Encoder, error) {
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
	args := buildFFmpegArgs(output, fps, resolvedFormat, opts)
	enc.cmd = exec.Command(ffmpegPath, args...) // #nosec G204 -- ffmpegPath is the configured renderer executable; args are not shell-expanded.
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

func buildFFmpegArgs(output string, fps int, format string, opts Options) []string {
	args := []string{
		"-f", "image2pipe",
		"-vcodec", "png",
		"-r", fmt.Sprintf("%d", fps),
		"-i", "pipe:0",
	}
	audioPath := strings.TrimSpace(opts.AudioPath)
	hasAudio := audioPath != ""
	if hasAudio {
		if opts.AudioLoop {
			args = append(args, "-stream_loop", "-1")
		}
		args = append(args, "-i", audioPath)
		if filter, ok := buildAudioFilter(opts); ok {
			args = append(args, "-filter_complex", filter, "-map", "0:v:0", "-map", "[cetus_audio]")
		} else {
			args = append(args, "-map", "0:v:0", "-map", "1:a:0?")
		}
	}

	switch format {
	case "webm":
		args = append(args,
			"-vcodec", "libvpx-vp9",
			"-pix_fmt", "yuva420p",
			"-b:v", "0",
			"-crf", "30",
		)
		if hasAudio {
			args = append(args, "-c:a", "libopus", "-b:a", "160k")
		} else {
			args = append(args, "-an")
		}
	default:
		args = append(args,
			"-vcodec", "libx264",
			"-pix_fmt", "yuv420p",
		)
		if hasAudio {
			args = append(args, "-c:a", "aac", "-b:a", "192k")
		} else {
			args = append(args, "-an")
		}
		args = append(args, "-movflags", "+faststart")
	}

	if hasAudio && opts.DurationSeconds > 0 {
		args = append(args, "-t", strconv.FormatFloat(opts.DurationSeconds, 'f', -1, 64))
	}

	args = append(args, "-y", output)
	return args
}

func buildAudioFilter(opts Options) (string, bool) {
	var filters []string
	volume := opts.AudioVolume
	if !opts.AudioVolumeSet && volume == 0 {
		volume = 1
	}
	if volume != 1 {
		filters = append(filters, "volume="+formatFilterFloat(volume))
	}
	if opts.AudioFadeInSeconds > 0 {
		filters = append(filters, "afade=t=in:st=0:d="+formatFilterFloat(opts.AudioFadeInSeconds))
	}
	if opts.AudioFadeOutSeconds > 0 {
		fadeStart := opts.DurationSeconds - opts.AudioStartSeconds - opts.AudioFadeOutSeconds
		if fadeStart < 0 {
			fadeStart = 0
		}
		filters = append(filters, "afade=t=out:st="+formatFilterFloat(fadeStart)+":d="+formatFilterFloat(opts.AudioFadeOutSeconds))
	}
	if opts.AudioStartSeconds > 0 {
		delayMS := int64(opts.AudioStartSeconds*1000 + 0.5)
		filters = append(filters, "adelay="+strconv.FormatInt(delayMS, 10)+":all=1")
	}
	if len(filters) == 0 {
		return "", false
	}
	return "[1:a:0]" + strings.Join(filters, ",") + "[cetus_audio]", true
}

func formatFilterFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
