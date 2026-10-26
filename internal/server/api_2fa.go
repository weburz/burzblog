package server

import (
	"errors"
	"net/http"

	"github.com/weburz/burzblog/internal/auth"
)

// twoFactorIssuer names BurzBlog in authenticator apps.
const twoFactorIssuer = "BurzBlog"

type twoFactorReply struct {
	Enabled       bool `json:"enabled"`
	Pending       bool `json:"pending"`
	RecoveryCodes int  `json:"recoveryCodes"`
}

func (s *Server) twoFactorStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.auth.TwoFactor(r.Context(), who(r).user)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	reply(w, http.StatusOK, twoFactorReply{Enabled: st.Enabled, Pending: st.Pending, RecoveryCodes: st.RecoveryCodes})
}

// passwordFailed answers a failed password check on the account pages and
// reports whether err was one.
func (s *Server) passwordFailed(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, auth.ErrBusy):
		busy(w)
	case errors.Is(err, auth.ErrCredentials):
		fail(w, http.StatusBadRequest, "credentials", "Your current password is not correct.", "")
	case errors.Is(err, auth.ErrRateLimited):
		fail(w, http.StatusTooManyRequests, "rate_limited", "Too many failed attempts. Wait 15 minutes, then try again.", "")
	default:
		return false
	}
	return true
}

func (s *Server) twoFactorStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	user := who(r).user
	secret, err := s.auth.StartTwoFactor(clientContext(r), user, body.Password)
	switch {
	case s.passwordFailed(w, r, err):
	case errors.Is(err, auth.ErrTwoFactorOn):
		fail(w, http.StatusConflict, "two_factor_on", "Two-factor sign-in is already on. Turn it off first to set it up again.", "")
	case err != nil:
		s.internal(w, r, err)
	default:
		reply(w, http.StatusOK, map[string]string{
			"secret": secret,
			"uri":    auth.TOTPURI(twoFactorIssuer, user.Username+"@"+s.origin.Host, secret),
		})
	}
}

func (s *Server) twoFactorConfirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &body) {
		return
	}
	codes, err := s.auth.ConfirmTwoFactor(r.Context(), who(r).user, body.Code)
	switch {
	case errors.Is(err, auth.ErrCode):
		fail(w, http.StatusBadRequest, "code", "That code is not right. Check the latest code in your authenticator app.", "")
	case errors.Is(err, auth.ErrTwoFactorOff):
		fail(w, http.StatusConflict, "two_factor_off", "Start the setup again to get a new secret.", "")
	case err != nil:
		s.internal(w, r, err)
	default:
		s.record(r, "2fa.enable", "")
		reply(w, http.StatusOK, map[string]any{"enabled": true, "recoveryCodes": codes})
	}
}

func (s *Server) twoFactorDisable(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decode(w, r, &body) {
		return
	}
	id := who(r)
	keep := ""
	if id.session != nil {
		keep = id.session.TokenHash
	}
	err := s.auth.DisableTwoFactor(clientContext(r), id.user, body.Password, body.Code, keep)
	switch {
	case s.passwordFailed(w, r, err):
	case errors.Is(err, auth.ErrTwoFactorOff):
		fail(w, http.StatusConflict, "two_factor_off", "Two-factor sign-in is not on.", "")
	case s.codeFailed(w, r, err):
	default:
		if auth.IsRecoveryCode(body.Code) {
			s.record(r, "2fa.recovery", "")
		}
		s.record(r, "2fa.disable", "")
		reply(w, http.StatusOK, map[string]bool{"enabled": false})
	}
}
