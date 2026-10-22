//go:build unix

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// forceSaveFailure makes the next save of the store at path fail by taking
// write permission away from its directory.
func forceSaveFailure(t *testing.T, path string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
}
