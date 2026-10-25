// Package server is BurzBlog's HTTP layer: the JSON API under /api, the
// embedded browser panel, security headers and the reader deploy hook.
package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/weburz/burzblog/internal/auth"
	"github.com/weburz/burzblog/internal/store"
)

// Config describes one BurzBlog instance.
type Config struct {
	// Origin is the public URL people open, e.g. https://blog.example.com.
	// Browser mutations must come from it.
	Origin string
	// DeployHook is POSTed after published content changes (optional).
	DeployHook string
	// Panel is the built browser panel (index.html and _nuxt/). Nil serves
	// a short notice instead.
	Panel fs.FS
	// Version is reported by /healthz.
	Version string
	Logger  *slog.Logger
}

// Server handles HTTP requests.
type Server struct {
	cfg       Config
	origin    *url.URL
	store     *store.Store
	auth      *auth.Service
	panel     *panel
	deploy    *deployer
	scheduler *scheduler
	log       *slog.Logger
	mux       *http.ServeMux
}

// New builds a Server.
func New(cfg Config, s *store.Store, a *auth.Service) (*Server, error) {
	origin, err := ParseOrigin(cfg.Origin)
	if err != nil {
		return nil, err
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	srv := &Server{cfg: cfg, origin: origin, store: s, auth: a, log: cfg.Logger, mux: http.NewServeMux()}
	if srv.panel, err = newPanel(cfg.Panel); err != nil {
		return nil, err
	}
	if cfg.DeployHook != "" {
		hook, err := url.Parse(cfg.DeployHook)
		if err != nil || (hook.Scheme != "https" && hook.Scheme != "http") || hook.Host == "" {
			return nil, errors.New("deploy hook must be a full http(s) URL")
		}
	}
	srv.deploy = newDeployer(cfg.DeployHook, cfg.Logger)
	srv.scheduler = newScheduler()
	srv.routes()
	return srv, nil
}

// ParseOrigin validates a public origin like https://blog.example.com.
func ParseOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil {
		return nil, fmt.Errorf("origin must look like https://blog.example.com, got %q", raw)
	}
	u.Path = ""
	return u, nil
}

// Close waits for the scheduler to stop (its context must be cancelled), then
// flushes a pending deploy.
func (s *Server) Close(ctx context.Context) {
	done := make(chan struct{})
	go func() { s.scheduler.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
	s.deploy.close(ctx)
}

// Handler returns the root handler with every middleware applied.
func (s *Server) Handler() http.Handler {
	return s.recover(s.headers(s.mux))
}

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /healthz", s.healthz)

	m.HandleFunc("GET /api/session", s.withIdentity(s.session))
	m.HandleFunc("POST /api/login", s.login)
	m.HandleFunc("POST /api/login/totp", s.loginTOTP)
	m.HandleFunc("POST /api/logout", s.logout)
	m.HandleFunc("POST /api/auth/token", s.issueCLIToken)

	m.HandleFunc("GET /api/tokens", s.write(s.listTokens))
	m.HandleFunc("POST /api/tokens", s.write(s.createToken))
	m.HandleFunc("DELETE /api/tokens/{id}", s.write(s.deleteToken))
	m.HandleFunc("POST /api/account/password", s.write(s.changePassword))
	m.HandleFunc("GET /api/account/2fa", s.write(s.twoFactorStatus))
	m.HandleFunc("POST /api/account/2fa/start", s.write(s.twoFactorStart))
	m.HandleFunc("POST /api/account/2fa/confirm", s.write(s.twoFactorConfirm))
	m.HandleFunc("POST /api/account/2fa/disable", s.write(s.twoFactorDisable))
	m.HandleFunc("GET /api/settings", s.write(s.settings))
	m.HandleFunc("POST /api/deploy", s.write(s.triggerDeploy))

	m.HandleFunc("GET /api/posts", s.write(s.listPosts))
	m.HandleFunc("GET /api/tags", s.write(s.listTags))
	m.HandleFunc("POST /api/posts", s.write(s.createPost))
	m.HandleFunc("GET /api/posts/{slug}", s.write(s.getPost))
	m.HandleFunc("PUT /api/posts/{slug}", s.write(s.savePost))
	m.HandleFunc("DELETE /api/posts/{slug}", s.write(s.deletePost))
	m.HandleFunc("POST /api/posts/{slug}/{action}", s.write(s.postAction))
	m.HandleFunc("GET /api/posts/{slug}/markdown", s.write(s.getMarkdown))
	m.HandleFunc("PUT /api/posts/{slug}/markdown", s.write(s.putMarkdown))
	m.HandleFunc("GET /api/posts/{slug}/revisions", s.write(s.listRevisions))
	m.HandleFunc("GET /api/posts/{slug}/revisions/{version}", s.write(s.getRevision))
	m.HandleFunc("POST /api/posts/{slug}/revisions/{version}/restore", s.write(s.restoreRevision))
	m.HandleFunc("POST /api/preview", s.write(s.preview))
	m.HandleFunc("GET /api/export.tar.gz", s.reader(s.export))
	m.HandleFunc("GET /api/content/posts", s.reader(s.contentList))
	m.HandleFunc("GET /api/content/posts/{slug}", s.reader(s.contentPost))
	m.HandleFunc("GET /api/content/posts/{slug}/html", s.reader(s.contentHTML))
	m.HandleFunc("GET /api/content/tags", s.reader(s.contentTags))
	m.HandleFunc("GET /api/backup.tar.gz", s.write(s.backup))

	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound, "not_found", "There is no such API endpoint.", "")
	})
	m.Handle("/", s.panel)
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		s.log.Error("health check failed", "err", err)
		fail(w, http.StatusServiceUnavailable, "unavailable", "The database is not answering.", "")
		return
	}
	reply(w, http.StatusOK, map[string]string{"status": "ok", "version": s.cfg.Version})
}

// headers applies security headers everywhere; the panel adds its CSP.
func (s *Server) headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Robots-Tag", "noindex, nofollow")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		if s.origin.Scheme == "https" {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			h.Set("Cache-Control", "no-store")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("handler panic", "path", r.URL.Path, "panic", v, "stack", string(debug.Stack()))
				fail(w, http.StatusInternalServerError, "internal", "Something went wrong on the server. Your last change may not be saved.", "")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
