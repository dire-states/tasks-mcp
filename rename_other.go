//go:build !windows

package main

import "os"

// renameFile renames oldpath over newpath atomically. It is a hook only
// because Windows needs retries; see rename_windows.go.
func renameFile(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}
