package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/weburz/burzblog/internal/auth"
	"github.com/weburz/burzblog/internal/store"
)

// identity is who is making a request, and how they proved it.
type identity struct {
	user    *store.User
	session *store.Session // set for browser cookies
	token   *store.Token   // set for bearer tokens
}

type identityKey struct{}

func who(r *http.Request) *identity {
	id, _ := r.Context().Value(identityKey{}).(*identity)
	return id
}

func (s *Server) cookieName() string {
	// The __Host- prefix pins the cookie to this exact origin; browsers only
	// accept it over HTTPS.
	if s.origin.Scheme == "https" {
		return "__Host-burzblog"
	}
	return "burzblog_session"
}

// resolve finds the caller from a bearer token or a session cookie.
func (s *Server) resolve(r *http.Request) (*identity, error) {
	if h := r.Header.Get("Authorization"); h != "" {
		secret, ok := strings.CutPrefix(h, "Bearer ")
		if !ok {
			return nil, auth.ErrNoSession
		}
		tok, err := s.auth.Token(r.Context(), strings.TrimSpace(secret))
		if err != nil {
			return nil, err
		}
		user, err := s.store.UserByID(r.Context(), tok.UserID)
		if err != nil {
			return nil, err
		}
		return &identity{user: user, token: tok}, nil
	}
	c, err := r.Cookie(s.cookieName())
	if err != nil {
		return nil, auth.ErrNoSession
	}
	sess, user, err := s.auth.Session(r.Context(), c.Value)
	if err != nil {
		return nil, err
	}
	return &identity{user: user, session: sess}, nil
}

// withIdentity attaches the caller if there is one, without requiring it.
// Export tokens may only read published content, so here they stay anonymous.
func (s *Server) withIdentity(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := s.resolve(r)
		if err != nil && !errors.Is(err, auth.ErrNoSession) {
			s.internal(w, r, err)
			return
		}
		if id != nil && id.token != nil && id.token.Scope != store.ScopeWrite {
			id = nil
		}
		if id != nil {
			r = r.WithContext(context.WithValue(r.Context(), identityKey{}, id))
		}
		next(w, r)
	}
}

// write requires a signed-in browser (with CSRF and origin checks on
// changes) or a bearer token with the write scope.
func (s *Server) write(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := s.resolve(r)
		if errors.Is(err, auth.ErrNoSession) {
			fail(w, http.StatusUnauthorized, "authentication", "Sign in to continue.", "")
			return
		}
		if err != nil {
			s.internal(w, r, err)
			return
		}
		if id.token != nil && id.token.Scope != store.ScopeWrite {
			fail(w, http.StatusForbidden, "scope", "This token can only read published posts.", "")
			return
		}
		if id.session != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !s.sameOrigin(r) {
				s.failOrigin(w)
				return
			}
			if !auth.CheckCSRF(id.session, r.Header.Get("X-CSRF-Token")) {
				fail(w, http.StatusForbidden, "csrf", "Your session token is out of date. Reload the page and try again.", "")
				return
			}
		}
		next(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	}
}

// reader allows any valid token or session, export tokens included; used for
// the export and the read-only content API that website builds call.
func (s *Server) reader(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := s.resolve(r)
		if errors.Is(err, auth.ErrNoSession) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="burzblog"`)
			fail(w, http.StatusUnauthorized, "authentication", "Send an API token as 'Authorization: Bearer <token>'.", "")
			return
		}
		if err != nil {
			s.internal(w, r, err)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	}
}

// sameOrigin requires the Origin header, which browsers send on every
// non-GET fetch, to match the configured public origin.
func (s *Server) sameOrigin(r *http.Request) bool {
	return r.Header.Get("Origin") == s.origin.Scheme+"://"+s.origin.Host
}

// failOrigin rejects a request from the wrong origin. The configured origin is
// public, so the message names it: the usual cause is opening the panel as
// localhost when the server expects 127.0.0.1.
func (s *Server) failOrigin(w http.ResponseWriter) {
	fail(w, http.StatusForbidden, "origin",
		"This request did not come from the BurzBlog page. Open BurzBlog at "+s.origin.Scheme+"://"+s.origin.Host+" and try again.", "")
}

// Request bodies are small: the largest is one post.
const maxBody = 4 << 20

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		fail(w, http.StatusUnsupportedMediaType, "content_type", "Send JSON with Content-Type: application/json.", "")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fail(w, http.StatusRequestEntityTooLarge, "too_large", "That is more than BurzBlog accepts in one request.", "")
			return false
		}
		fail(w, http.StatusBadRequest, "bad_request", "The request was not valid JSON for this endpoint.", err.Error())
		return false
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		fail(w, http.StatusBadRequest, "bad_request", "Send exactly one JSON object.", "")
		return false
	}
	return true
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// fail writes the standard error shape. message tells people what to do;
// detail names the specific cause when there is one.
func fail(w http.ResponseWriter, status int, code, message, detail string) {
	reply(w, status, map[string]apiError{"error": {Code: code, Message: message, Detail: detail}})
}

func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	fail(w, http.StatusInternalServerError, "internal", "Something went wrong on the server. Your last change may not be saved.", "")
}
