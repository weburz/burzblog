package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/weburz/burzblog/internal/post"
	"github.com/weburz/burzblog/internal/render"
	"github.com/weburz/burzblog/internal/store"
)

// The content API is the read-only view of published posts for the website:
// the same data the export holds, one post at a time. Drafts, scheduled and
// archived posts answer exactly like a slug that does not exist.

// wordsPerMinute is the reading speed behind readingMinutes.
const wordsPerMinute = 200

// contentReply is a published post as the content API returns it. Body is only
// set on the single-post route. contentJSON is the one place that maps a post
// to it, so a new post field is added here and appears in both routes.
type contentReply struct {
	Slug           string     `json:"slug"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	CoverURL       string     `json:"coverUrl"`
	CoverAlt       string     `json:"coverAlt"`
	PublishedOn    *time.Time `json:"publishedOn"`
	UpdatedOn      *time.Time `json:"updatedOn"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	Tags           []string   `json:"tags"`
	Author         string     `json:"author"`
	SEOTitle       string     `json:"seoTitle"`
	SEODescription string     `json:"seoDescription"`
	CanonicalURL   string     `json:"canonicalUrl"`
	Words          int        `json:"words"`
	ReadingMinutes int        `json:"readingMinutes"`
	Version        int64      `json:"version"`
	Body           *string    `json:"body,omitempty"`
}

func contentJSON(p *post.Post, words int, withBody bool) contentReply {
	c := contentReply{
		Slug: p.Slug, Title: p.Title, Description: p.Description, CoverURL: p.CoverURL, CoverAlt: p.CoverAlt,
		Tags: p.Tags, Author: p.Author, SEOTitle: p.SEOTitle, SEODescription: p.SEODescription,
		CanonicalURL: p.CanonicalURL,
		PublishedOn:  p.PublishedOn, UpdatedOn: p.UpdatedOn, UpdatedAt: p.UpdatedAt, Words: words,
		ReadingMinutes: (words + wordsPerMinute - 1) / wordsPerMinute, Version: p.Version,
	}
	if c.Tags == nil {
		c.Tags = []string{}
	}
	if withBody {
		c.Body = &p.Body
	}
	return c
}

// contentCaching is the Cache-Control of content responses: the same caller
// may reuse them briefly. They are private because the request carries a
// token, and shared caches must not hand one caller's response to another.
const contentCaching = "private, max-age=60"

// cached sets the validators of a successful content response.
func cached(w http.ResponseWriter, tag string) {
	h := w.Header()
	h.Set("ETag", tag)
	h.Set("Cache-Control", contentCaching)
	h.Set("Vary", "Authorization, Cookie")
}

// notModified answers 304 and reports true when the caller already holds the
// representation tagged tag.
func notModified(w http.ResponseWriter, r *http.Request, tag string) bool {
	for _, candidate := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == tag || candidate == "*" {
			cached(w, tag)
			w.WriteHeader(http.StatusNotModified)
			return true
		}
	}
	return false
}

// publishedPost loads a published post, or answers 404 for a missing slug and
// for any other status alike.
func (s *Server) publishedPost(w http.ResponseWriter, r *http.Request) (*post.Post, bool) {
	slug := r.PathValue("slug")
	p, err := s.store.GetPost(r.Context(), slug)
	if err == nil && p.Status != post.Published {
		err = store.ErrNotFound
	}
	if errors.Is(err, store.ErrNotFound) && s.movedPost(w, r, slug) {
		return nil, false
	}
	if err != nil {
		s.postError(w, r, err, nil)
		return nil, false
	}
	return p, true
}

// contentList lists published posts, newest published first, paged like
// /api/posts. Without ?limit= every match is returned.
func (s *Server) contentList(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := pageParams(w, r)
	if !ok {
		return
	}
	list, total, err := s.store.ListPublished(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("tag"), limit, offset)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	items := make([]contentReply, 0, len(list))
	for _, l := range list {
		items = append(items, contentJSON(l.Post, l.Words, false))
	}
	s.replyDigest(w, r, map[string]any{"items": items, "total": total})
}

// contentPost returns one published post: JSON with the Markdown body, or with
// ?format=markdown the frontmatter document the export holds for it.
func (s *Server) contentPost(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format != "" && format != "json" && format != "markdown" {
		fail(w, http.StatusUnprocessableEntity, "validation", "Format must be json or markdown.", "format")
		return
	}
	p, ok := s.publishedPost(w, r)
	if !ok {
		return
	}
	if format == "markdown" {
		tag := strings.TrimSuffix(etag(p.Version), `"`) + `-md"`
		if notModified(w, r, tag) {
			return
		}
		cached(w, tag)
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = io.WriteString(w, post.Encode(p))
		return
	}
	if notModified(w, r, etag(p.Version)) {
		return
	}
	cached(w, etag(p.Version))
	reply(w, http.StatusOK, contentJSON(p, post.CountWords(p.Body), true))
}

// contentHTML returns one published post's body rendered and sanitized like a
// preview, so it shares the preview's input limits and render slots.
func (s *Server) contentHTML(w http.ResponseWriter, r *http.Request) {
	p, ok := s.publishedPost(w, r)
	if !ok {
		return
	}
	tag := strings.TrimSuffix(etag(p.Version), `"`) + `-html"`
	if notModified(w, r, tag) {
		return
	}
	html, err := render.HTML(r.Context(), p.Body)
	if !s.renderError(w, r, err) {
		return
	}
	cached(w, tag)
	reply(w, http.StatusOK, map[string]any{"slug": p.Slug, "version": p.Version, "html": html})
}

// contentTags lists the tags of published posts and how many published posts
// carry each, so a website can build tag pages.
func (s *Server) contentTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.PublishedTags(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(tags))
	for _, t := range tags {
		items = append(items, map[string]any{"tag": t.Tag, "count": t.Count})
	}
	s.replyDigest(w, r, map[string]any{"items": items})
}

// replyDigest answers v as JSON tagged with a digest of its encoding, for
// responses that no single version number describes.
func (s *Server) replyDigest(w http.ResponseWriter, r *http.Request, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	sum := sha256.Sum256(raw)
	tag := `"` + hex.EncodeToString(sum[:8]) + `"`
	if notModified(w, r, tag) {
		return
	}
	cached(w, tag)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(append(raw, '\n'))
}
