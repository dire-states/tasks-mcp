//go:build unix

package main

import (
	"os"
	"syscall"
)

// Lock modes for lockFile.
const (
	lockShared    = syscall.LOCK_SH
	lockExclusive = syscall.LOCK_EX
)

// lockFile takes an advisory flock(2) lock on f in the given mode, blocking
// until it is available.
func lockFile(f *os.File, how int) error {
	return syscall.Flock(int(f.Fd()), how)
}

// unlockFile releases the lock taken by lockFile.
func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
