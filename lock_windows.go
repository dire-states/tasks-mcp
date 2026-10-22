//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// Lock modes for lockFile.
const (
	lockShared    = 0
	lockExclusive = windows.LOCKFILE_EXCLUSIVE_LOCK
)

// allBytes selects the whole file: offset 0, maximum length. Locking beyond
// the end of the (empty) lock file is valid on Windows.
const allBytes = ^uint32(0)

// lockFile takes a LockFileEx lock on f in the given mode, blocking until it
// is available. Locks are owned per handle, so two Stores or two processes
// contend with each other just like flock. Unlike flock, a second lock on
// the same handle would block forever; Store.mu guarantees that never
// happens.
func lockFile(f *os.File, how int) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), uint32(how), 0, allBytes, allBytes, new(windows.Overlapped))
}

// unlockFile releases the lock taken by lockFile. The range must match the
// locked one exactly.
func unlockFile(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, allBytes, allBytes, new(windows.Overlapped))
}
