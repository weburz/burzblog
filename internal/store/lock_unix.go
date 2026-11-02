//go:build unix

package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockDir takes an advisory flock on LockFile without waiting. The kernel
// drops it when the process ends, however it ends.
func lockDir(dir, name string, exclusive bool) (func(), error) {
	// Read-only and world-readable: flock needs no write access, so a lock
	// file a root restore created still opens for the service user. It holds
	// nothing.
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_RDONLY|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("lock the data directory: %w", err)
	}
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	if err := syscall.Flock(int(f.Fd()), how|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrInUse
		}
		return nil, fmt.Errorf("lock the data directory: %w", err)
	}
	return func() { f.Close() }, nil
}
