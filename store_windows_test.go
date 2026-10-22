//go:build windows

package main

import (
	"os"
	"testing"
	"time"
)

// forceSaveFailure makes the next save of the store at path fail by holding
// the file open: Windows will not rename over an open file. The rename retry
// window is shortened so the test does not wait for it.
func forceSaveFailure(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })

	prev := renameRetryWindow
	renameRetryWindow = 50 * time.Millisecond
	t.Cleanup(func() { renameRetryWindow = prev })
}
