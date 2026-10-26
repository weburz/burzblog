package server

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/weburz/burzblog/internal/media"
	"github.com/weburz/burzblog/internal/post"
)

// coverMediaName returns the file name of the upload that a cover URL points
// to: a path under /media/ on this server, relative or absolute with this
// server's origin. It returns "" for any other URL, including an upload on
// another server.
func (s *Server) coverMediaName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" {
		return ""
	}
	if u.Scheme != "" || u.Host != "" {
		if !strings.EqualFold(u.Scheme, s.origin.Scheme) || !strings.EqualFold(u.Host, s.origin.Host) {
			return ""
		}
	}
	name, ok := strings.CutPrefix(u.Path, mediaPrefix)
	if !ok || !media.ValidName(name) {
		return ""
	}
	return name
}

// coverSizes looks up, in one query, the pixel size of every cover among posts
// that points to a decoded upload, by file name.
func (s *Server) coverSizes(ctx context.Context, posts []*post.Post) (map[string]coverSize, error) {
	var names []string
	seen := map[string]bool{}
	for _, p := range posts {
		if name := s.coverMediaName(p.CoverURL); name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	sizes, err := s.store.MediaSizes(ctx, names)
	if err != nil {
		return nil, err
	}
	out := make(map[string]coverSize, len(sizes))
	for name, size := range sizes {
		out[name] = coverSize{size.Width, size.Height}
	}
	return out, nil
}

// coverSize is the pixel size of a cover that is an upload.
type coverSize struct{ width, height int }

// setCover fills the cover size of c from sizes, when p's cover has one.
func (s *Server) setCover(c *contentReply, p *post.Post, sizes map[string]coverSize) {
	if size, ok := sizes[s.coverMediaName(p.CoverURL)]; ok {
		c.CoverWidth, c.CoverHeight = size.width, size.height
	}
}

// extraJSON returns the extra frontmatter of p as JSON values: always an
// object. Extra that cannot be read (the editor validates it, but an import or
// an older release may have stored what it would refuse now) is logged and
// answered as {}, so one bad post does not fail a whole response.
func (s *Server) extraJSON(r *http.Request, slug, src string) map[string]any {
	values, err := post.ExtraValues(src)
	if err != nil {
		s.log.Warn("content API: unreadable extra frontmatter", "slug", slug, "err", err, "path", r.URL.Path)
		return map[string]any{}
	}
	return values
}

// includeExtra reports whether ?include= asks for extra on a list. It answers
// 422 and returns false for a value it does not know, so a typo is not
// silently ignored.
func includeExtra(w http.ResponseWriter, r *http.Request) (extra, ok bool) {
	for _, v := range r.URL.Query()["include"] {
		for _, item := range strings.Split(v, ",") {
			switch strings.TrimSpace(item) {
			case "extra":
				extra = true
			case "":
			default:
				fail(w, http.StatusUnprocessableEntity, "validation", "Include may only be extra.", "")
				return false, false
			}
		}
	}
	return extra, true
}

// tagWithCover returns the ETag of a single post with the cover size appended
// when it has one, because an upload's size is not part of the post's version.
func tagWithCover(version int64, c contentReply) string {
	if c.CoverWidth == 0 {
		return etag(version)
	}
	return `"` + strconv.FormatInt(version, 10) + "-" + strconv.Itoa(c.CoverWidth) + "x" + strconv.Itoa(c.CoverHeight) + `"`
}
