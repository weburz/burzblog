//go:build windows

package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func lockDir(dir, name string, exclusive bool) (func(), error) {
	// OPEN_ALWAYS requires write access even to an existing read-only file.
	// Once the lock file exists, shared readers need only GENERIC_READ.
	path := filepath.Join(dir, name)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		f, err = os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0o644)
	}
	if err != nil {
		return nil, fmt.Errorf("lock the data directory: %w", err)
	}
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	var region windows.Overlapped
	handle := windows.Handle(f.Fd())
	if err := windows.LockFileEx(handle, flags, 0, 1, 0, &region); err != nil {
		f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrInUse
		}
		return nil, fmt.Errorf("lock the data directory: %w", err)
	}
	return func() { windows.UnlockFileEx(handle, 0, 1, 0, &region); f.Close() }, nil
}
