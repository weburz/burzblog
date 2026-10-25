package server

import (
	"errors"
	"net/http"

	"github.com/weburz/burzblog/internal/auth"
	"github.com/weburz/burzblog/internal/store"
)

// setupStatus tells the panel whether to offer first-run setup. It reveals
// only whether any account exists, nothing about tokens.
func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	needed, err := s.auth.SetupNeeded(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	reply(w, http.StatusOK, map[string]bool{"needed": needed})
}

// setup creates the first account from the one-time token `burzblog serve`
// printed, then signs the browser in. It is the only unauthenticated write,
// and it is closed as soon as any account exists.
func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		fail(w, http.StatusForbidden, "origin", "This request did not come from the BurzBlog page.", "")
		return
	}
	var body struct {
		Token string `json:"token"`
		credentials
	}
	if !decode(w, r, &body) {
		return
	}
	// Same rules as `burzblog user add`. A bad name or password says nothing
	// about the token and does not use up an attempt.
	if err := auth.CheckUsername(store.NormalizeUsername(body.Username)); err != nil {
		fail(w, http.StatusBadRequest, "validation", "The account could not be created.", err.Error()+".")
		return
	}
	if err := auth.CheckPassword(body.Password); err != nil {
		fail(w, http.StatusBadRequest, "validation", "The account could not be created.", err.Error()+".")
		return
	}
	user, err := s.auth.Setup(clientContext(r), body.Token, body.Username, body.Password)
	switch {
	case errors.Is(err, auth.ErrBusy):
		busy(w)
	case errors.Is(err, auth.ErrRateLimited):
		w.Header().Set("Retry-After", "900")
		fail(w, http.StatusTooManyRequests, "rate_limited", "Too many failed attempts. Wait 15 minutes, then try again.", "")
	case errors.Is(err, auth.ErrSetupDone):
		fail(w, http.StatusConflict, "setup_done", "This server already has an account. Sign in instead.", "")
	case errors.Is(err, auth.ErrSetupToken):
		fail(w, http.StatusForbidden, "setup_token", "This setup link is not valid. It may have expired or been used already; restart `burzblog serve` for a new one.", "")
	case err != nil:
		s.internal(w, r, err)
	default:
		s.startSession(w, r, user, http.StatusCreated)
	}
}
