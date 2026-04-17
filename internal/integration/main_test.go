package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// cairnBinary holds the path to a freshly built cairn binary used by all
// integration tests in this package. Built once in TestMain.
var cairnBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cairn-it-*")
	if err != nil {
		panic(err)
	}
	name := "cairn"
	if runtime.GOOS == "windows" {
		name = "cairn.exe"
	}
	cairnBinary = filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", cairnBinary, "./cmd/cairn")
	// Run from repo root.
	wd, _ := os.Getwd()
	cmd.Dir = filepath.Dir(filepath.Dir(wd)) // internal/integration → repo root
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		os.RemoveAll(dir)
		panic("failed to build cairn: " + err.Error())
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
