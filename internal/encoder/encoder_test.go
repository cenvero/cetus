package encoder

import (
	"reflect"
	"testing"
)

func TestResolveFormat(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		explicit string
		want     string
	}{
		{name: "explicit mp4", output: "out.webm", explicit: "mp4", want: "mp4"},
		{name: "webm extension", output: "out.webm", want: "webm"},
		{name: "mp4 fallback", output: "out.mov", want: "mp4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveFormat(tt.output, tt.explicit)
			if err != nil {
				t.Fatalf("ResolveFormat returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ResolveFormat() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveFormatRejectsUnknown(t *testing.T) {
	if _, err := ResolveFormat("out.mp4", "gif"); err == nil {
		t.Fatal("ResolveFormat returned nil error")
	}
}

func TestBuildFFmpegArgsWithoutAudio(t *testing.T) {
	got := buildFFmpegArgs("out.mp4", 30, "mp4", Options{})
	want := []string{
		"-f", "image2pipe",
		"-vcodec", "png",
		"-r", "30",
		"-i", "pipe:0",
		"-vcodec", "libx264",
		"-pix_fmt", "yuv420p",
		"-an",
		"-movflags", "+faststart",
		"-y", "out.mp4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildFFmpegArgs() = %#v, want %#v", got, want)
	}
}

func TestBuildFFmpegArgsWithMP4Audio(t *testing.T) {
	got := buildFFmpegArgs("out.mp4", 60, "mp4", Options{
		AudioPath:       "music.mp3",
		DurationSeconds: 15.5,
	})
	want := []string{
		"-f", "image2pipe",
		"-vcodec", "png",
		"-r", "60",
		"-i", "pipe:0",
		"-i", "music.mp3",
		"-map", "0:v:0",
		"-map", "1:a:0?",
		"-vcodec", "libx264",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "192k",
		"-movflags", "+faststart",
		"-t", "15.5",
		"-y", "out.mp4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildFFmpegArgs() = %#v, want %#v", got, want)
	}
}

func TestBuildFFmpegArgsWithWebMAudio(t *testing.T) {
	got := buildFFmpegArgs("out.webm", 24, "webm", Options{
		AudioPath:       "voiceover.wav",
		DurationSeconds: 3,
	})
	want := []string{
		"-f", "image2pipe",
		"-vcodec", "png",
		"-r", "24",
		"-i", "pipe:0",
		"-i", "voiceover.wav",
		"-map", "0:v:0",
		"-map", "1:a:0?",
		"-vcodec", "libvpx-vp9",
		"-pix_fmt", "yuva420p",
		"-b:v", "0",
		"-crf", "30",
		"-c:a", "libopus",
		"-b:a", "160k",
		"-t", "3",
		"-y", "out.webm",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildFFmpegArgs() = %#v, want %#v", got, want)
	}
}

func TestBuildFFmpegArgsWithAudioControls(t *testing.T) {
	got := buildFFmpegArgs("out.mp4", 30, "mp4", Options{
		AudioPath:           "music.mp3",
		AudioVolume:         0.7,
		AudioVolumeSet:      true,
		AudioLoop:           true,
		AudioStartSeconds:   2.5,
		AudioFadeInSeconds:  1,
		AudioFadeOutSeconds: 2,
		DurationSeconds:     10,
	})
	want := []string{
		"-f", "image2pipe",
		"-vcodec", "png",
		"-r", "30",
		"-i", "pipe:0",
		"-stream_loop", "-1",
		"-i", "music.mp3",
		"-filter_complex", "[1:a:0]volume=0.7,afade=t=in:st=0:d=1,afade=t=out:st=5.5:d=2,adelay=2500:all=1[cetus_audio]",
		"-map", "0:v:0",
		"-map", "[cetus_audio]",
		"-vcodec", "libx264",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "192k",
		"-movflags", "+faststart",
		"-t", "10",
		"-y", "out.mp4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildFFmpegArgs() = %#v, want %#v", got, want)
	}
}
