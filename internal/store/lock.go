package store

import "errors"

// LockFile is the file in the data directory that a running server holds a
// shared lock on, so a restore can tell the data is in use.
const LockFile = "burzblog.lock"

// ErrInUse means another process holds a lock on the data directory that
// conflicts with the one asked for.
var ErrInUse = errors.New("the data directory is in use")

// LockShared takes a shared lock on the data directory for as long as a
// server runs. Several servers may hold it; a restore cannot start meanwhile.
// It returns ErrInUse while a restore runs.
func LockShared(dir string) (unlock func(), err error) {
	return lockDir(dir, false)
}

// LockExclusive takes the lock a restore needs: no server may run. It returns
// ErrInUse while a server runs on the directory.
func LockExclusive(dir string) (unlock func(), err error) {
	return lockDir(dir, true)
}
