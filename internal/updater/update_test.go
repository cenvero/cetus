package updater

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    int
		ok      bool
	}{
		{current: "v1.0.0", latest: "v1.0.1", want: -1, ok: true},
		{current: "1.2.0", latest: "v1.1.9", want: 1, ok: true},
		{current: "v1.2.3", latest: "1.2.3", want: 0, ok: true},
		{current: "dev", latest: "v1.0.0", want: 0, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.current+"_"+tt.latest, func(t *testing.T) {
			got, ok := compareVersions(tt.current, tt.latest)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("compareVersions() = (%d, %v), want (%d, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	got, ok := parseVersion("v1.2.3-beta.1")
	if !ok {
		t.Fatal("parseVersion returned !ok")
	}
	if got != [3]int{1, 2, 3} {
		t.Fatalf("parseVersion = %#v, want [1 2 3]", got)
	}
}
