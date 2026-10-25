package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/weburz/burzblog/internal/auth"
	"github.com/weburz/burzblog/internal/store"
)

type sessionReply struct {
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username,omitempty"`
	CSRF          string `json:"csrf,omitempty"`
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	id := who(r)
	if id == nil {
		reply(w, http.StatusOK, sessionReply{})
		return
	}
	out := sessionReply{Authenticated: true, Username: id.user.Username}
	if id.session != nil {
		out.CSRF = id.session.CSRF
	}
	reply(w, http.StatusOK, out)
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// clientContext tags the request context with the connecting address. It is
// RemoteAddr only: X-Forwarded-For is never trusted.
func clientContext(r *http.Request) context.Context {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return auth.WithClient(r.Context(), host)
}

func busy(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "5")
	fail(w, http.StatusServiceUnavailable, "busy", "BurzBlog is busy checking sign-ins. Try again in a few seconds.", "")
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request, c credentials) *store.User {
	user, err := s.auth.Authenticate(clientContext(r), c.Username, c.Password)
	switch {
	case errors.Is(err, auth.ErrBusy):
		busy(w)
	case errors.Is(err, auth.ErrRateLimited):
		w.Header().Set("Retry-After", "900")
		fail(w, http.StatusTooManyRequests, "rate_limited", "Too many failed sign-in attempts. Wait 15 minutes, then try again.", "")
	case errors.Is(err, auth.ErrCredentials):
		fail(w, http.StatusUnauthorized, "credentials", "That username and password do not match.", "")
	case err != nil:
		s.internal(w, r, err)
	default:
		return user
	}
	return nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	// A login form posted from another site could sign the victim into the
	// attacker's account; only accept it from our own page.
	if !s.sameOrigin(r) {
		fail(w, http.StatusForbidden, "origin", "This request did not come from the BurzBlog page.", "")
		return
	}
	var c credentials
	if !decode(w, r, &c) {
		return
	}
	user := s.authenticate(w, r, c)
	if user == nil {
		return
	}
	s.startSession(w, r, user, http.StatusOK)
}

// startSession signs the browser in as user: it sets the session cookie and
// replies with the CSRF token.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, user *store.User, status int) {
	token, sess, err := s.auth.NewSession(r.Context(), user)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: s.cookieName(), Value: token, Path: "/", Expires: sess.ExpiresAt,
		MaxAge: int(auth.SessionTTL.Seconds()), HttpOnly: true, Secure: s.origin.Scheme == "https",
		SameSite: http.SameSiteStrictMode,
	})
	reply(w, status, sessionReply{Authenticated: true, Username: user.Username, CSRF: sess.CSRF})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		fail(w, http.StatusForbidden, "origin", "This request did not come from the BurzBlog page.", "")
		return
	}
	if c, err := r.Cookie(s.cookieName()); err == nil {
		if err := s.auth.EndSession(r.Context(), c.Value); err != nil {
			s.internal(w, r, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: s.cookieName(), Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: s.origin.Scheme == "https", SameSite: http.SameSiteStrictMode,
	})
	reply(w, http.StatusOK, sessionReply{})
}

type tokenReply struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Scope      string     `json:"scope"`
	Username   string     `json:"username"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	// Secret is only present in the response that creates the token.
	Secret string `json:"secret,omitempty"`
}

func tokenJSON(t *store.Token) tokenReply {
	return tokenReply{ID: t.ID, Name: t.Name, Scope: t.Scope, Username: t.Username, CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt}
}

// issueCLIToken trades a username and password for a write token; this is
// what `burzblog login` calls.
func (s *Server) issueCLIToken(w http.ResponseWriter, r *http.Request) {
	// Browsers always send Origin on POST; refuse other sites' pages.
	if o := r.Header.Get("Origin"); o != "" && !s.sameOrigin(r) {
		fail(w, http.StatusForbidden, "origin", "This request did not come from the BurzBlog page.", "")
		return
	}
	var body struct {
		credentials
		Name string `json:"name"`
	}
	if !decode(w, r, &body) {
		return
	}
	user := s.authenticate(w, r, body.credentials)
	if user == nil {
		return
	}
	if body.Name == "" {
		body.Name = "CLI"
	}
	secret, t, err := s.auth.NewToken(r.Context(), user, body.Name, store.ScopeWrite)
	if err != nil {
		fail(w, http.StatusBadRequest, "validation", "The token could not be created.", err.Error())
		return
	}
	out := tokenJSON(t)
	out.Secret = secret
	reply(w, http.StatusCreated, out)
}

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Tokens(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]tokenReply, 0, len(list))
	for i := range list {
		out = append(out, tokenJSON(&list[i]))
	}
	reply(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	if !decode(w, r, &body) {
		return
	}
	secret, t, err := s.auth.NewToken(r.Context(), who(r).user, body.Name, body.Scope)
	if err != nil {
		fail(w, http.StatusBadRequest, "validation", "The token could not be created.", err.Error())
		return
	}
	out := tokenJSON(t)
	out.Secret = secret
	reply(w, http.StatusCreated, out)
}

func (s *Server) deleteToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusNotFound, "not_found", "That token does not exist.", "")
		return
	}
	if err := s.store.DeleteToken(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "not_found", "That token does not exist. It may already be revoked.", "")
		return
	} else if err != nil {
		s.internal(w, r, err)
		return
	}
	reply(w, http.StatusOK, map[string]bool{"revoked": true})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if !decode(w, r, &body) {
		return
	}
	id := who(r)
	keep := ""
	if id.session != nil {
		keep = id.session.TokenHash
	}
	err := s.auth.ChangePassword(clientContext(r), id.user, body.Current, body.Next, keep)
	switch {
	case errors.Is(err, auth.ErrBusy):
		busy(w)
	case errors.Is(err, auth.ErrCredentials):
		fail(w, http.StatusBadRequest, "credentials", "Your current password is not correct.", "")
	case errors.Is(err, auth.ErrRateLimited):
		fail(w, http.StatusTooManyRequests, "rate_limited", "Too many failed attempts. Wait 15 minutes, then try again.", "")
	case err != nil && auth.CheckPassword(body.Next) != nil:
		fail(w, http.StatusBadRequest, "validation", "Choose a stronger new password.", auth.CheckPassword(body.Next).Error()+".")
	case err != nil:
		s.internal(w, r, err)
	default:
		reply(w, http.StatusOK, map[string]bool{"changed": true})
	}
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, map[string]any{
		"username": who(r).user.Username,
		"origin":   s.origin.String(),
		"version":  s.cfg.Version,
		"deploy":   s.deploy.status(),
	})
}

func (s *Server) triggerDeploy(w http.ResponseWriter, r *http.Request) {
	if !s.deploy.configured() {
		fail(w, http.StatusConflict, "deploy_unconfigured", "No deploy hook is set. Start BurzBlog with --deploy-hook to rebuild your site from here.", "")
		return
	}
	s.deploy.now()
	reply(w, http.StatusAccepted, s.deploy.status())
}
