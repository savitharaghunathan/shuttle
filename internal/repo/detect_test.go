package repo

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDetectTestCommand(t *testing.T) {
	dir := t.TempDir()

	if got := DetectTestCommand(dir); !reflect.DeepEqual(got, []string{"true"}) {
		t.Fatalf("empty dir: got %v", got)
	}

	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com\n")
	if got := DetectTestCommand(dir); !reflect.DeepEqual(got, []string{"go", "test", "./..."}) {
		t.Fatalf("go.mod: got %v", got)
	}

	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir2, "package.json"), "{}")
	if got := DetectTestCommand(dir2); !reflect.DeepEqual(got, []string{"npm", "test"}) {
		t.Fatalf("package.json: got %v", got)
	}

	dir3 := t.TempDir()
	writeFile(t, filepath.Join(dir3, "pyproject.toml"), "[project]\n")
	if got := DetectTestCommand(dir3); !reflect.DeepEqual(got, []string{"python", "-m", "pytest"}) {
		t.Fatalf("pyproject.toml: got %v", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
