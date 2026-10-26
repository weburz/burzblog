package server

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/weburz/burzblog/internal/post"
	"github.com/weburz/burzblog/internal/store"
)

// remoteHost is the connecting address, without the port. It is RemoteAddr
// only: X-Forwarded-For is never trusted.
func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// forbidRole answers 403 to an account whose role may not do this.
func forbidRole(w http.ResponseWriter) {
	fail(w, http.StatusForbidden, "role",
		"Your account is an author, and this needs an admin. Ask an admin to do it, or to make you one.", "")
}

// isAdmin reports whether the caller's account is an admin. A token has the
// role of the account that owns it, read fresh on every request.
func isAdmin(r *http.Request) bool {
	id := who(r)
	return id != nil && id.user.Role == store.RoleAdmin
}

// adminOnly lets only admins through. The caller must already be signed in
// (wrapped in write or reader).
func adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidRole(w)
			return
		}
		next(w, r)
	}
}

// admin is write for routes only admins may use.
func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	return s.write(adminOnly(next))
}

// addAudit appends an entry. A failure is logged, not shown: the action it
// describes has already happened.
func (s *Server) addAudit(ctx context.Context, a store.Audit) {
	if err := s.store.AddAudit(ctx, a); err != nil {
		s.log.Error("audit entry not recorded", "action", a.Action, "err", err)
	}
}

// record logs an action of the signed-in caller. target must never hold a
// secret.
func (s *Server) record(r *http.Request, action, target string) {
	id := who(r)
	if id == nil {
		return
	}
	a := store.Audit{ActorUserID: id.user.ID, ActorName: id.user.Username, Action: action, Target: target, Remote: remoteHost(r)}
	if id.token != nil {
		a.TokenID = id.token.ID
	}
	s.addAudit(r.Context(), a)
}

// recordAs logs an action of a user who is not (yet) the request's identity,
// such as a sign-in.
func (s *Server) recordAs(r *http.Request, user *store.User, action, target string) {
	s.addAudit(r.Context(), store.Audit{ActorUserID: user.ID, ActorName: user.Username, Action: action, Target: target, Remote: remoteHost(r)})
}

// recordStatus logs a change of a post's status or schedule made by update.
func (s *Server) recordStatus(r *http.Request, before, after *post.Post) {
	action := ""
	switch {
	case before.Status != after.Status && after.Status == post.Published:
		action = "post.publish"
	case before.Status != after.Status && after.Status == post.Archived:
		action = "post.archive"
	case before.Status == post.Published && after.Status == post.Draft:
		action = "post.unpublish"
	case before.Status == post.Archived && after.Status == post.Draft:
		action = "post.restore"
	case before.PublishAt == nil && after.PublishAt != nil:
		action = "post.schedule"
	case before.PublishAt != nil && after.PublishAt == nil && after.Status == post.Draft:
		action = "post.unschedule"
	default:
		return
	}
	s.record(r, action, after.Slug)
}

type auditReply struct {
	ID      int64     `json:"id"`
	At      time.Time `json:"at"`
	ActorID int64     `json:"actorId,omitempty"`
	Actor   string    `json:"actor"`
	TokenID int64     `json:"tokenId,omitempty"`
	Action  string    `json:"action"`
	Target  string    `json:"target"`
	Remote  string    `json:"remote"`
}

// defaultAuditPage is how many entries GET /api/audit returns without ?limit=:
// the log only grows, so it is never sent whole.
const defaultAuditPage = 100

// listAudit pages the audit log, newest first. Admins only.
func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := pageParams(w, r)
	if !ok {
		return
	}
	if limit == 0 {
		limit = defaultAuditPage
	}
	entries, total, err := s.store.AuditLog(r.Context(), limit, offset)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	items := make([]auditReply, 0, len(entries))
	for _, e := range entries {
		items = append(items, auditReply{ID: e.ID, At: e.At, ActorID: e.ActorUserID, Actor: e.ActorName,
			TokenID: e.TokenID, Action: e.Action, Target: e.Target, Remote: e.Remote})
	}
	reply(w, http.StatusOK, map[string]any{"items": items, "total": total})
}
