package version

import (
	"fmt"
	"runtime"
)

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

func Platform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func String() string {
	return fmt.Sprintf("cetus %s (%s, %s, %s)", Version, Commit, Date, Platform())
}
