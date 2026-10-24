package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
)

// panel serves the built single-page app: fingerprinted assets cached
// forever, and index.html for every other path with a CSP that allows only
// its own inline bootstrap scripts.
type panel struct {
	files  fs.FS
	index  []byte
	policy string
}

var inlineScript = regexp.MustCompile(`(?s)<script([^>]*)>(.*?)</script>`)

const notBuilt = `<!doctype html><meta charset="utf-8"><title>BurzBlog</title>
<p>The browser panel is not built into this binary. Run <code>task build</code>, or use the API and CLI.</p>`

func newPanel(files fs.FS) (*panel, error) {
	p := &panel{files: files}
	if files != nil {
		index, err := fs.ReadFile(files, "index.html")
		if errors.Is(err, fs.ErrNotExist) {
			files = nil
		} else if err != nil {
			return nil, err
		} else {
			p.index = index
		}
	}
	if files == nil {
		p.files = nil
		p.index = []byte(notBuilt)
	}
	hashes := []string{}
	for _, m := range inlineScript.FindAllSubmatch(p.index, -1) {
		attrs, body := m[1], m[2]
		if bytes.Contains(attrs, []byte("src=")) || bytes.Contains(attrs, []byte("application/json")) {
			continue
		}
		sum := sha256.Sum256(body)
		hashes = append(hashes, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	sort.Strings(hashes)
	// Inline styles stay allowed: Nuxt UI sets style attributes, and the
	// preview iframe (srcdoc, sandboxed) carries its own stylesheet.
	p.policy = strings.Join([]string{
		"default-src 'none'",
		strings.TrimSpace("script-src 'self' " + strings.Join(hashes, " ")),
		"style-src 'self' 'unsafe-inline'",
		"font-src 'self' data:",
		"img-src 'self' data: https:",
		"connect-src 'self'",
		"frame-src 'self'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"object-src 'none'",
	}, "; ")
	return p, nil
}

func (p *panel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	// Every HTML document is the app shell, served with its CSP; generated
	// copies such as settings/index.html are never served as plain files.
	if p.files != nil && name != "" && !strings.HasSuffix(name, ".html") {
		if data, err := fs.ReadFile(p.files, name); err == nil {
			if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
				w.Header().Set("Content-Type", ct)
			}
			if strings.HasPrefix(name, "_nuxt/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			_, _ = w.Write(data)
			return
		}
		// A missing hashed asset is a stale tab after an upgrade, not a page.
		if strings.HasPrefix(name, "_nuxt/") || path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Security-Policy", p.policy)
	_, _ = w.Write(p.index)
}
