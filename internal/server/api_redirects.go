package server

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/weburz/burzblog/internal/store"
)

// contentPostsPrefix is where the content API serves posts by slug.
const contentPostsPrefix = "/api/content/posts/"

// redirectItem is one old address and the slug it now lives at.
type redirectItem struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// redirectItems maps store redirects to their JSON shape; it is never nil.
func redirectItems(list []store.Redirect) []redirectItem {
	items := make([]redirectItem, 0, len(list))
	for _, rd := range list {
		items = append(items, redirectItem{From: rd.From, To: rd.To})
	}
	return items
}

// contentRedirects lists the old slugs of renamed published posts with the
// slug each has now, so a website can redirect its old addresses. Redirects to
// drafts and archived posts are left out.
func (s *Server) contentRedirects(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.PublishedRedirects(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.replyDigest(w, r, map[string]any{"items": redirectItems(list)})
}

// movedPost answers a content request for an old slug of a published post with
// 308 and the same request for the current slug. It reports false, writing
// nothing, when slug is not such a slug, so a missing slug and one that now
// belongs to an unpublished post answer alike.
func (s *Server) movedPost(w http.ResponseWriter, r *http.Request, slug string) bool {
	to, err := s.store.RedirectTarget(r.Context(), slug)
	if err != nil {
		return false
	}
	location := contentPostsPrefix + url.PathEscape(to) + strings.TrimPrefix(r.URL.Path, contentPostsPrefix+slug)
	if r.URL.RawQuery != "" {
		location += "?" + r.URL.RawQuery
	}
	w.Header().Set("Location", location)
	reply(w, http.StatusPermanentRedirect, map[string]string{"redirect": to})
	return true
}

// redirectsFile is the export's list of redirects, next to the posts.
const redirectsFile = "redirects.json"

// addRedirects writes redirects.json, the same {"items":[{from,to}]} the
// content API answers, to the export.
func (s *Server) addRedirects(ctx context.Context, tw *tar.Writer) error {
	list, err := s.store.PublishedRedirects(ctx)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(map[string]any{"items": redirectItems(list)}, "", "  ")
	if err != nil {
		return err
	}
	return addToTar(tw, redirectsFile, string(raw)+"\n", s.store.Now())
}

// listPostRedirects lists the old slugs that redirect to a post, whatever
// its status, for the panel.
func (s *Server) listPostRedirects(w http.ResponseWriter, r *http.Request) {
	from, err := s.store.PostRedirects(r.Context(), r.PathValue("slug"))
	if err != nil {
		s.postError(w, r, err, nil)
		return
	}
	reply(w, http.StatusOK, map[string]any{"items": from})
}

// deleteRedirect removes one old slug's redirect to a post.
func (s *Server) deleteRedirect(w http.ResponseWriter, r *http.Request) {
	err := s.store.DeleteRedirect(r.Context(), r.PathValue("slug"), r.PathValue("from"))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "not_found", "That redirect does not exist. It may already be removed.", "")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.record(r, "redirect.delete", r.PathValue("from")+" to "+r.PathValue("slug"))
	reply(w, http.StatusOK, map[string]bool{"deleted": true})
}
