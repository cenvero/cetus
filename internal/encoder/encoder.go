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
	Quality             int    // CRF value; 0 means use codec default
	Scale               string // ffmpeg scale filter value, e.g. "scale=-2:720"
	FrameCodec          string // input frame codec: "" or "png" (default) or "webp"
	SubtitlesPath       string // path to an SRT or ASS subtitle file to burn in
}

// ParseScale converts a user-supplied scale string to an ffmpeg scale filter value.
// Accepted values: 480p, 720p, 1080p, 4k, or WxH (e.g. 1920x1080). Empty string is valid and means no scaling.
func ParseScale(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return "", nil
	case "480p":
		return "scale=-2:480", nil
	case "720p":
		return "scale=-2:720", nil
	case "1080p":
		return "scale=-2:1080", nil
	case "4k":
		return "scale=-2:2160", nil
	default:
		parts := strings.SplitN(strings.ToLower(s), "x", 2)
		if len(parts) == 2 {
			w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
			h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
			if errW == nil && errH == nil && w > 0 && h > 0 {
				return fmt.Sprintf("scale=%d:%d", w, h), nil
			}
		}
		return "", fmt.Errorf("invalid scale %q; use 480p, 720p, 1080p, 4k, or WxH (e.g. 1920x1080)", s)
	}
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

func (e *Encoder) WriteFrame(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("frame data is empty")
	}
	if _, err := e.stdin.Write(data); err != nil {
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

// ExtractFrame converts a single image file to an output image using ffmpeg.
func ExtractFrame(ffmpegPath, inputPath, outputPath string) error {
	var stderr bytes.Buffer
	cmd := exec.Command(ffmpegPath, "-i", inputPath, "-y", outputPath) // #nosec G204
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("ffmpeg extract frame: %w: %s", err, msg)
		}
		return fmt.Errorf("ffmpeg extract frame: %w", err)
	}
	return nil
}

func ResolveFormat(output, explicit string) (string, error) {
	format := strings.ToLower(strings.TrimSpace(explicit))
	if format == "" {
		switch strings.ToLower(filepath.Ext(output)) {
		case ".mp4":
			format = "mp4"
		case ".webm":
			format = "webm"
		default:
			ext := filepath.Ext(output)
			if ext == "" {
				return "", fmt.Errorf("output file %q has no extension; use .mp4 or .webm, or pass --format", output)
			}
			return "", fmt.Errorf("unrecognized output extension %q; use .mp4 or .webm, or pass --format", ext)
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
	frameCodec := opts.FrameCodec
	if frameCodec == "" {
		frameCodec = "png"
	}

	args := []string{
		"-f", "image2pipe",
		"-vcodec", frameCodec,
		"-r", strconv.Itoa(fps),
		"-i", "pipe:0",
	}

	audioPath := strings.TrimSpace(opts.AudioPath)
	hasAudio := audioPath != ""
	var audioFilter string
	var hasComplexAudio bool
	if hasAudio {
		audioFilter, hasComplexAudio = buildAudioFilter(opts)
	}

	if hasAudio {
		if opts.AudioLoop {
			args = append(args, "-stream_loop", "-1")
		}
		args = append(args, "-i", audioPath)
	}

	// Wire video and audio streams, incorporating scale and/or subtitle filters.
	videoFilter := buildVideoFilter(opts)
	switch {
	case videoFilter != "" && hasComplexAudio:
		combined := "[0:v:0]" + videoFilter + "[vout];" + audioFilter
		args = append(args, "-filter_complex", combined, "-map", "[vout]", "-map", "[cetus_audio]")
	case videoFilter != "" && hasAudio:
		args = append(args, "-filter_complex", "[0:v:0]"+videoFilter+"[vout]", "-map", "[vout]", "-map", "1:a:0?")
	case hasComplexAudio:
		args = append(args, "-filter_complex", audioFilter, "-map", "0:v:0", "-map", "[cetus_audio]")
	case hasAudio:
		args = append(args, "-map", "0:v:0", "-map", "1:a:0?")
	case videoFilter != "":
		args = append(args, "-vf", videoFilter)
	}

	switch format {
	case "webm":
		crfVal := 30
		if opts.Quality > 0 {
			crfVal = opts.Quality
		}
		args = append(args,
			"-vcodec", "libvpx-vp9",
			"-pix_fmt", "yuva420p",
			"-b:v", "0",
			"-crf", strconv.Itoa(crfVal),
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
		if opts.Quality > 0 {
			args = append(args, "-crf", strconv.Itoa(opts.Quality))
		}
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

// buildVideoFilter combines scale and subtitle filters into one ffmpeg filter chain string.
func buildVideoFilter(opts Options) string {
	var parts []string
	if opts.Scale != "" {
		parts = append(parts, opts.Scale)
	}
	if strings.TrimSpace(opts.SubtitlesPath) != "" {
		abs, err := filepath.Abs(opts.SubtitlesPath)
		if err != nil {
			abs = opts.SubtitlesPath
		}
		parts = append(parts, "subtitles="+escapeSubtitlesPath(abs))
	}
	return strings.Join(parts, ",")
}

// escapeSubtitlesPath escapes a file path for use inside an ffmpeg filter expression.
// Forward slashes are used on all platforms; special filter characters are backslash-escaped.
func escapeSubtitlesPath(p string) string {
	p = filepath.ToSlash(p)
	p = strings.ReplaceAll(p, "\\", "\\\\")
	p = strings.ReplaceAll(p, ":", "\\:")
	p = strings.ReplaceAll(p, "'", "\\'")
	p = strings.ReplaceAll(p, ",", "\\,")
	p = strings.ReplaceAll(p, "[", "\\[")
	p = strings.ReplaceAll(p, "]", "\\]")
	return "'" + p + "'"
}
