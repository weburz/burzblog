package server

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/weburz/burzblog/internal/post"
	"github.com/weburz/burzblog/internal/render"
	"github.com/weburz/burzblog/internal/store"
)

type postReply struct {
	Slug        string      `json:"slug"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Status      post.Status `json:"status"`
	Body        string      `json:"body"`
	CoverURL    string      `json:"coverUrl"`
	CoverAlt    string      `json:"coverAlt"`
	Extra       string      `json:"extra"`
	PublishedOn *time.Time  `json:"publishedOn"`
	UpdatedOn   *time.Time  `json:"updatedOn"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
	UpdatedBy   string      `json:"updatedBy"`
	Version     int64       `json:"version"`
}

func postJSON(p *post.Post) postReply {
	return postReply{
		Slug: p.Slug, Title: p.Title, Description: p.Description, Status: p.Status, Body: p.Body,
		CoverURL: p.CoverURL, CoverAlt: p.CoverAlt, Extra: p.Extra, PublishedOn: p.PublishedOn,
		UpdatedOn: p.UpdatedOn, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, UpdatedBy: p.UpdatedBy,
		Version: p.Version,
	}
}

type summaryReply struct {
	Slug        string      `json:"slug"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Status      post.Status `json:"status"`
	Words       int         `json:"words"`
	PublishedOn *time.Time  `json:"publishedOn"`
	UpdatedAt   time.Time   `json:"updatedAt"`
	UpdatedBy   string      `json:"updatedBy"`
	Version     int64       `json:"version"`
}

// postError maps store and validation errors to responses. current is the
// post as it is now, sent with conflicts so clients can show what changed.
func (s *Server) postError(w http.ResponseWriter, r *http.Request, err error, current *post.Post) {
	var fe *post.FieldError
	switch {
	case errors.As(err, &fe):
		fail(w, http.StatusUnprocessableEntity, "validation", "Fix the highlighted field, then save again.", fe.Error())
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "not_found", "That post does not exist. It may have been renamed or deleted.", "")
	case errors.Is(err, store.ErrTaken):
		fail(w, http.StatusConflict, "slug_taken", "Another post already uses that slug. Choose a different one.", "")
	case errors.Is(err, store.ErrState):
		fail(w, http.StatusConflict, "state", "Archive the post before deleting it.", "")
	case errors.Is(err, store.ErrConflict):
		body := map[string]any{"error": apiError{
			Code:    "conflict",
			Message: "This post changed since you opened it. Review the newer version before saving.",
		}}
		if current != nil {
			body["current"] = postJSON(current)
		}
		reply(w, http.StatusConflict, body)
	default:
		s.internal(w, r, err)
	}
}

func (s *Server) listPosts(w http.ResponseWriter, r *http.Request) {
	status := post.Status(r.URL.Query().Get("status"))
	if status != "" && !post.ValidStatus(status) {
		fail(w, http.StatusBadRequest, "bad_request", "Status must be draft, published or archived.", "")
		return
	}
	list, counts, err := s.store.ListPosts(r.Context(), store.Filter{Status: status, Query: r.URL.Query().Get("q")})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	items := make([]summaryReply, 0, len(list))
	for _, p := range list {
		items = append(items, summaryReply{
			Slug: p.Slug, Title: p.Title, Description: p.Description, Status: p.Status, Words: p.Words,
			PublishedOn: p.PublishedOn, UpdatedAt: p.UpdatedAt, UpdatedBy: p.UpdatedBy, Version: p.Version,
		})
	}
	reply(w, http.StatusOK, map[string]any{
		"items":  items,
		"counts": map[string]int{"draft": counts.Draft, "published": counts.Published, "archived": counts.Archived},
	})
}

type postInput struct {
	Version     int64      `json:"version"`
	Slug        string     `json:"slug"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Body        string     `json:"body"`
	CoverURL    string     `json:"coverUrl"`
	CoverAlt    string     `json:"coverAlt"`
	Extra       string     `json:"extra"`
	PublishedOn *time.Time `json:"publishedOn"`
	UpdatedOn   *time.Time `json:"updatedOn"`
}

func (in *postInput) apply(p *post.Post) {
	if in.Slug != "" {
		p.Slug = in.Slug
	}
	p.Title, p.Description, p.Body = in.Title, in.Description, in.Body
	p.CoverURL, p.CoverAlt, p.Extra = in.CoverURL, in.CoverAlt, in.Extra
	p.PublishedOn, p.UpdatedOn = in.PublishedOn, in.UpdatedOn
}

func (s *Server) createPost(w http.ResponseWriter, r *http.Request) {
	var in postInput
	if !decode(w, r, &in) {
		return
	}
	p := &post.Post{Status: post.Draft}
	in.apply(p)
	if p.Slug == "" {
		p.Slug = post.Slugify(p.Title)
	}
	created, err := s.store.CreatePost(r.Context(), p, who(r).user.Username)
	if err != nil {
		s.postError(w, r, err, nil)
		return
	}
	reply(w, http.StatusCreated, postJSON(created))
}

func (s *Server) getPost(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPost(r.Context(), r.PathValue("slug"))
	if err != nil {
		s.postError(w, r, err, nil)
		return
	}
	reply(w, http.StatusOK, postJSON(p))
}

func (s *Server) savePost(w http.ResponseWriter, r *http.Request) {
	var in postInput
	if !decode(w, r, &in) {
		return
	}
	s.update(w, r, r.PathValue("slug"), in.Version, func(p *post.Post) error {
		in.apply(p)
		return nil
	})
}

// update runs a versioned change and triggers a reader rebuild when
// published content changed.
func (s *Server) update(w http.ResponseWriter, r *http.Request, slug string, version int64, change func(*post.Post) error) {
	var before post.Post
	updated, err := s.store.UpdatePost(r.Context(), slug, version, who(r).user.Username, func(p *post.Post) error {
		before = *p
		return change(p)
	})
	if err != nil {
		s.postError(w, r, err, updated)
		return
	}
	if updated.Version != before.Version && (before.Status == post.Published || updated.Status == post.Published) {
		s.deploy.schedule()
	}
	reply(w, http.StatusOK, postJSON(updated))
}

func (s *Server) postAction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version int64 `json:"version"`
	}
	if !decode(w, r, &body) {
		return
	}
	action := r.PathValue("action")
	var next post.Status
	var from []post.Status
	switch action {
	case "publish":
		next, from = post.Published, []post.Status{post.Draft}
	case "unpublish":
		next, from = post.Draft, []post.Status{post.Published}
	case "archive":
		next, from = post.Archived, []post.Status{post.Draft, post.Published}
	case "restore":
		next, from = post.Draft, []post.Status{post.Archived}
	default:
		fail(w, http.StatusNotFound, "not_found", "There is no such post action.", "")
		return
	}
	s.update(w, r, r.PathValue("slug"), body.Version, func(p *post.Post) error {
		for _, f := range from {
			if p.Status == f {
				p.Status = next
				return nil
			}
		}
		return &post.FieldError{Field: "status", Message: fmt.Sprintf("is %s, so the post cannot be %s.", p.Status, pastTense(action))}
	})
}

func pastTense(action string) string {
	switch action {
	case "publish":
		return "published again"
	case "unpublish":
		return "unpublished"
	case "archive":
		return "archived again"
	default:
		return "restored"
	}
}

func (s *Server) deletePost(w http.ResponseWriter, r *http.Request) {
	version, err := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
	if err != nil {
		fail(w, http.StatusBadRequest, "bad_request", "Pass the post version you are deleting as ?version=N.", "")
		return
	}
	if err := s.store.DeletePost(r.Context(), r.PathValue("slug"), version); err != nil {
		s.postError(w, r, err, nil)
		return
	}
	reply(w, http.StatusOK, map[string]bool{"deleted": true})
}

func etag(version int64) string {
	return `"` + strconv.FormatInt(version, 10) + `"`
}

func (s *Server) getMarkdown(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPost(r.Context(), r.PathValue("slug"))
	if err != nil {
		s.postError(w, r, err, nil)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("ETag", etag(p.Version))
	w.Header().Set("X-BurzBlog-Status", string(p.Status))
	_, _ = io.WriteString(w, post.Encode(p))
}

// putMarkdown saves a whole frontmatter document, as the CLI edits it.
// If-Match carries the version the edit started from; If-None-Match: *
// creates a new post instead.
func (s *Server) putMarkdown(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		fail(w, http.StatusRequestEntityTooLarge, "too_large", "That post is more than BurzBlog accepts.", "")
		return
	}
	doc, err := post.Decode(string(raw))
	if err != nil {
		s.postError(w, r, err, nil)
		return
	}
	slug := r.PathValue("slug")
	if doc.Slug != "" && doc.Slug != slug {
		s.postError(w, r, &post.FieldError{Field: "slug", Message: fmt.Sprintf(
			"in the frontmatter is %q but the post is %q; remove the slug line or make them match.", doc.Slug, slug)}, nil)
		return
	}
	if r.Header.Get("If-None-Match") == "*" {
		p := &post.Post{Slug: slug}
		doc.Apply(p)
		created, err := s.store.CreatePost(r.Context(), p, who(r).user.Username)
		if err != nil {
			s.postError(w, r, err, nil)
			return
		}
		if created.Status == post.Published {
			s.deploy.schedule()
		}
		w.Header().Set("ETag", etag(created.Version))
		reply(w, http.StatusCreated, postJSON(created))
		return
	}
	version, err := strconv.ParseInt(strings.Trim(r.Header.Get("If-Match"), `"`), 10, 64)
	if err != nil {
		fail(w, http.StatusPreconditionRequired, "version_required", "Send the version you edited in an If-Match header.", "")
		return
	}
	s.update(w, r, slug, version, func(p *post.Post) error {
		switch {
		case !doc.HasStatus:
			// A file without a status line keeps the post's status; it must
			// not quietly unpublish anything.
			doc.Status = p.Status
		case p.Status == post.Archived && doc.Status == post.Published:
			return &post.FieldError{Field: "status", Message: "cannot go from archived to published in one step; restore the post first."}
		case p.Status == post.Archived:
			// Saving an archived post from a file keeps it archived.
			doc.Status = post.Archived
		}
		doc.Apply(p)
		return nil
	})
}

type revisionReply struct {
	Version int64       `json:"version"`
	Title   string      `json:"title"`
	Status  post.Status `json:"status"`
	SavedAt time.Time   `json:"savedAt"`
	SavedBy string      `json:"savedBy"`
	Post    *postReply  `json:"post,omitempty"`
}

// revisionState is what the panel needs to say what a save changed: the
// fields it shows, a word count and short digests of the rest.
type revisionState struct {
	Slug              string      `json:"slug"`
	Title             string      `json:"title"`
	Status            post.Status `json:"status"`
	Words             int         `json:"words"`
	DescriptionDigest string      `json:"descriptionDigest"`
	CoverDigest       string      `json:"coverDigest"`
	SettingsDigest    string      `json:"settingsDigest"`
}

type revisionSummary struct {
	Version int64     `json:"version"`
	SavedAt time.Time `json:"savedAt"`
	SavedBy string    `json:"savedBy"`
	revisionState
}

// digest is a short fingerprint of parts: equal inputs give equal digests.
func digest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:6])
}

func stamp(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func stateOf(p *post.Post, words int) revisionState {
	return revisionState{
		Slug: p.Slug, Title: p.Title, Status: p.Status, Words: words,
		DescriptionDigest: digest(p.Description),
		CoverDigest:       digest(p.CoverURL, p.CoverAlt),
		SettingsDigest:    digest(p.Extra, stamp(p.PublishedOn), stamp(p.UpdatedOn)),
	}
}

func (s *Server) listRevisions(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	list, err := s.store.Revisions(r.Context(), slug)
	if err != nil {
		s.postError(w, r, err, nil)
		return
	}
	current, err := s.store.GetPost(r.Context(), slug)
	if err != nil {
		s.postError(w, r, err, nil)
		return
	}
	items := make([]revisionSummary, 0, len(list))
	for _, rev := range list {
		items = append(items, revisionSummary{Version: rev.Version, SavedAt: rev.SavedAt, SavedBy: rev.SavedBy,
			revisionState: stateOf(&rev.Post, rev.Words)})
	}
	reply(w, http.StatusOK, map[string]any{"items": items, "current": stateOf(current, post.CountWords(current.Body))})
}

func (s *Server) revision(w http.ResponseWriter, r *http.Request) *store.Revision {
	version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil {
		fail(w, http.StatusNotFound, "not_found", "That version does not exist.", "")
		return nil
	}
	rev, err := s.store.Revision(r.Context(), r.PathValue("slug"), version)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "not_found", "That version does not exist.", "")
		return nil
	}
	if err != nil {
		s.internal(w, r, err)
		return nil
	}
	return rev
}

func (s *Server) getRevision(w http.ResponseWriter, r *http.Request) {
	rev := s.revision(w, r)
	if rev == nil {
		return
	}
	p := postJSON(&rev.Post)
	reply(w, http.StatusOK, revisionReply{Version: rev.Version, Title: rev.Post.Title, Status: rev.Post.Status, SavedAt: rev.SavedAt, SavedBy: rev.SavedBy, Post: &p})
}

// restoreRevision brings back an earlier version's content as a new
// version. The slug and status stay as they are now.
func (s *Server) restoreRevision(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version int64 `json:"version"`
	}
	if !decode(w, r, &body) {
		return
	}
	rev := s.revision(w, r)
	if rev == nil {
		return
	}
	s.update(w, r, r.PathValue("slug"), body.Version, func(p *post.Post) error {
		old := rev.Post
		p.Title, p.Description, p.Body = old.Title, old.Description, old.Body
		p.CoverURL, p.CoverAlt, p.Extra = old.CoverURL, old.CoverAlt, old.Extra
		p.PublishedOn, p.UpdatedOn = old.PublishedOn, old.UpdatedOn
		return nil
	})
}

func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Markdown string `json:"markdown"`
	}
	if !decode(w, r, &body) {
		return
	}
	html, err := render.HTML(body.Markdown)
	if err != nil {
		fail(w, http.StatusRequestEntityTooLarge, "too_large", "The post is too large to preview.", "")
		return
	}
	reply(w, http.StatusOK, map[string]string{"html": html})
}

// export streams every published post as <slug>.md in a gzipped tar, for
// reader builds.
func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	posts, err := s.store.PublishedPosts(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="burzblog-export.tar.gz"`)
	w.Header().Set("X-BurzBlog-Posts", strconv.Itoa(len(posts)))
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	for _, p := range posts {
		doc := post.Encode(p)
		hdr := &tar.Header{
			Name: p.Slug + ".md", Mode: 0o644, Size: int64(len(doc)), ModTime: p.UpdatedAt,
			Typeflag: tar.TypeReg, Format: tar.FormatPAX,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			s.log.Error("export failed", "err", err)
			return
		}
		if _, err := io.WriteString(tw, doc); err != nil {
			s.log.Error("export failed", "err", err)
			return
		}
	}
	if err := tw.Close(); err != nil {
		s.log.Error("export failed", "err", err)
		return
	}
	if err := gz.Close(); err != nil {
		s.log.Error("export failed", "err", err)
	}
}
