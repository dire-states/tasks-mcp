//go:build windows

package main

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// renameRetryWindow bounds how long renameFile keeps retrying. A variable so
// tests can shorten it.
var renameRetryWindow = 2 * time.Second

// renameFile renames oldpath over newpath. Windows refuses to rename a file
// that any process has open, and an antivirus scanner, the search indexer or
// an editor can briefly hold either file, so the resulting transient errors
// are retried with exponential backoff, as Go's own toolchain does.
func renameFile(oldpath, newpath string) error {
	deadline := time.Now().Add(renameRetryWindow)
	delay := time.Millisecond
	for {
		err := os.Rename(oldpath, newpath)
		if err == nil || !isEphemeral(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(delay)
		if delay < 100*time.Millisecond {
			delay *= 2
		}
	}
}

// isEphemeral reports whether err is a sharing error that clears once the
// other handle is closed.
func isEphemeral(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
