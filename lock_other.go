//go:build !unix && !windows

package main

import (
	"errors"
	"os"
	"runtime"
)

// Lock modes for lockFile.
const (
	lockShared = iota
	lockExclusive
)

var errNoLocking = errors.New("file locking is not supported on " + runtime.GOOS)

// lockFile always fails: there is no file locking implementation for this
// platform, and OpenStore reports that at startup rather than letting the
// store run unprotected.
func lockFile(*os.File, int) error {
	return errNoLocking
}

// unlockFile is a no-op counterpart to lockFile.
func unlockFile(*os.File) error {
	return nil
}
