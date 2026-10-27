//go:build unix

package snapshot

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// matchOwner gives the restored files the owner of the data directory when
// root restored them, so a server running as its own user can open them.
func matchOwner(dataDir string, paths ...string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	info, err := os.Stat(dataDir)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid == 0 {
		return nil
	}
	for _, p := range paths {
		if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		err := filepath.WalkDir(p, func(path string, _ fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return os.Lchown(path, int(st.Uid), int(st.Gid))
		})
		if err != nil {
			return fmt.Errorf("give the restored files to the data directory's owner: %w", err)
		}
	}
	return nil
}
