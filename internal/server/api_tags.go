package server

import (
	"errors"
	"net/http"

	"github.com/weburz/burzblog/internal/post"
	"github.com/weburz/burzblog/internal/store"
)

// tagLabelsFor maps each of tags to the label to show: the one an admin set in
// overrides, or the label derived from the tag. The map is never nil.
func tagLabelsFor(overrides map[string]string, tags []string) map[string]string {
	labels := make(map[string]string, len(tags))
	for _, tag := range tags {
		labels[tag] = tagLabel(overrides, tag)
	}
	return labels
}

func tagLabel(overrides map[string]string, tag string) string {
	if label, ok := overrides[tag]; ok {
		return label
	}
	return post.DefaultTagLabel(tag)
}

// labelledTag is the tag a label belongs to, as the panel API returns it.
// Custom is false while the label is the one derived from the tag.
type labelledTag struct {
	Tag    string `json:"tag"`
	Label  string `json:"label"`
	Custom bool   `json:"custom"`
}

// labelledTagRoute reads and checks the {tag} of a label route, answering 422
// for a tag that is not normalized.
func labelledTagRoute(w http.ResponseWriter, r *http.Request) (string, bool) {
	tag := r.PathValue("tag")
	if tag == "" || post.NormalizeTag(tag) != tag {
		fail(w, http.StatusUnprocessableEntity, "validation",
			"A tag may only use lowercase letters, digits and single hyphens between words, like constitutional-law.", "tag")
		return "", false
	}
	return tag, true
}

// setTagLabel gives a tag in use a display label. Admins only.
func (s *Server) setTagLabel(w http.ResponseWriter, r *http.Request) {
	tag, ok := labelledTagRoute(w, r)
	if !ok {
		return
	}
	var in struct {
		Label string `json:"label"`
	}
	if !decode(w, r, &in) {
		return
	}
	label, err := s.store.SetTagLabel(r.Context(), tag, in.Label, who(r).user.Username)
	var fe *post.FieldError
	switch {
	case errors.As(err, &fe):
		fail(w, http.StatusUnprocessableEntity, "validation", "Fix the label, then save again.", fe.Error())
		return
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "not_found", "No post uses that tag, so it cannot be labelled.", "")
		return
	case err != nil:
		s.internal(w, r, err)
		return
	}
	s.record(r, "tag.label", tag)
	s.deploy.schedule()
	reply(w, http.StatusOK, labelledTag{Tag: tag, Label: label, Custom: true})
}

// clearTagLabel removes a tag's label so the derived one shows again. Clearing
// a tag that has no label succeeds. Admins only.
func (s *Server) clearTagLabel(w http.ResponseWriter, r *http.Request) {
	tag, ok := labelledTagRoute(w, r)
	if !ok {
		return
	}
	if err := s.store.ClearTagLabel(r.Context(), tag); err != nil {
		s.internal(w, r, err)
		return
	}
	s.record(r, "tag.label", tag+" (cleared)")
	s.deploy.schedule()
	reply(w, http.StatusOK, labelledTag{Tag: tag, Label: post.DefaultTagLabel(tag)})
}
