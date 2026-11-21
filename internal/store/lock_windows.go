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
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_RDONLY|os.O_CREATE, 0o644)
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
