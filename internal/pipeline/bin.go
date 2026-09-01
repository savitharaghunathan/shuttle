package pipeline

import "os"

var osExecutable = os.Executable

func ShuttleBin() string {
	if p := os.Getenv("SHUTTLE_BIN"); p != "" {
		return p
	}
	if p, err := osExecutable(); err == nil && p != "" {
		return p
	}
	return "shuttle"
}
