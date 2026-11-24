package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/weburz/burzblog/internal/post"
	"github.com/weburz/burzblog/internal/store"
)

// The manifest travels with the export directory. On the first export from an
// older installation there is no record of which files BurzBlog wrote.
func writeExport(dir string, posts, site map[string][]byte, clean bool) (int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	state := filepath.Join(dir, ".burzblog-export")
	if err := os.Mkdir(state, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return 0, err
	}
	info, err := os.Lstat(state)
	if err != nil || !info.IsDir() {
		return 0, fmt.Errorf("export state must be a directory at %s", state)
	}
	unlock, err := store.LockExclusive(state)
	if errors.Is(err, store.ErrInUse) {
		return 0, conflictf("another export is writing %s", dir)
	}
	if err != nil {
		return 0, err
	}
	defer unlock()

	path := filepath.Join(state, "posts.json")
	owned := map[string]string{}
	raw, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(raw, &owned); err != nil || owned == nil {
			return 0, fmt.Errorf("invalid export manifest at %s", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	for name := range owned {
		slug, ok := strings.CutSuffix(name, ".md")
		if !ok || post.ValidateSlug(slug) != nil {
			return 0, fmt.Errorf("invalid post filename in export manifest at %s", path)
		}
	}
	for _, set := range []map[string][]byte{posts, site} {
		for name, data := range set {
			if err := writeFileAtomic(filepath.Join(dir, name), data, 0o644); err != nil {
				return 0, err
			}
		}
	}
	removed := 0
	if clean {
		for name, hash := range owned {
			if _, published := posts[name]; published {
				continue
			}
			path := filepath.Join(dir, name)
			info, err := os.Lstat(path)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return 0, err
			}
			if err == nil && info.Mode().IsRegular() && info.Size() <= post.MaxBody+post.MaxExtra {
				content, err := os.ReadFile(path)
				if err != nil {
					return 0, err
				}
				if contentHash(content) == hash {
					if err := os.Remove(path); err != nil {
						return 0, err
					}
					removed++
				}
			}
			delete(owned, name)
		}
	}
	for name, contents := range posts {
		owned[name] = contentHash(contents)
	}
	raw, err = json.Marshal(owned)
	if err != nil {
		return 0, err
	}
	return removed, writeFileAtomic(path, raw, 0o600)
}
