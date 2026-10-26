package media

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Disk stores files in one directory.
type Disk struct {
	dir string
}

// DirName is the folder inside the data directory that holds uploads.
const DirName = "media"

// NewDisk returns a Disk rooted at dir, creating it (private to the service
// user) when needed.
func NewDisk(dir string) (*Disk, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create media directory: %w", err)
	}
	return &Disk{dir: dir}, nil
}

func (d *Disk) path(name string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("invalid media name %q", name)
	}
	return filepath.Join(d.dir, name), nil
}

// Put implements Storage. It writes a temporary file and renames it, so
// readers see all of the file or none.
func (d *Disk) Put(name string, data []byte) error {
	path, err := d.path(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	tmp, err := os.CreateTemp(d.dir, ".upload-*")
	if err != nil {
		return fmt.Errorf("store upload: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("store upload: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("store upload: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("store upload: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("store upload: %w", err)
	}
	return nil
}

// Open implements Storage.
func (d *Disk) Open(name string) (File, time.Time, error) {
	path, err := d.path(name)
	if err != nil {
		return nil, time.Time{}, ErrNotFound
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, time.Time{}, ErrNotFound
	}
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("open upload: %w", err)
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, time.Time{}, ErrNotFound
	}
	return f, info.ModTime(), nil
}

// Delete implements Storage.
func (d *Disk) Delete(name string) error {
	path, err := d.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("delete upload: %w", err)
	}
	return nil
}
