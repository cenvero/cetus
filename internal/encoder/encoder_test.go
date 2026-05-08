package encoder

import "testing"

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
