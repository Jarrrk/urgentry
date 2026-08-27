package web

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	authpkg "urgentry/internal/auth"
	"urgentry/internal/requestmeta"
	"urgentry/internal/sqlite"
)

var webInviteAcceptLimiter = authpkg.NewFixedWindowRateLimiter(time.Minute)

type acceptInviteData struct {
	Title   string
	Token   string
	Error   string
	Success bool
}

func (h *Handler) acceptInvitePage(w http.ResponseWriter, r *http.Request) {
	if h.admin == nil {
		http.Error(w, "Invitations unavailable", http.StatusServiceUnavailable)
		return
	}
	token := strings.TrimSpace(r.PathValue("invite_token"))
	if token == "" {
		http.NotFound(w, r)
		return
	}
	h.renderAcceptInvite(w, acceptInviteData{Title: "Accept Invitation", Token: token})
}

func (h *Handler) acceptInviteAction(w http.ResponseWriter, r *http.Request) {
	if h.admin == nil {
		http.Error(w, "Invitations unavailable", http.StatusServiceUnavailable)
		return
	}
	token := strings.TrimSpace(r.PathValue("invite_token"))
	now := time.Now().UTC()
	if _, allowed := webInviteAcceptLimiter.Allow("web-invite:ip:"+requestmeta.ClientIP(r), 10, now); !allowed {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too many invite attempts", http.StatusTooManyRequests)
		return
	}
	tokenHash := sha256.Sum256([]byte(token))
	if _, allowed := webInviteAcceptLimiter.Allow("web-invite:token:"+hex.EncodeToString(tokenHash[:8]), 5, now); !allowed {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too many invite attempts", http.StatusTooManyRequests)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderAcceptInvite(w, acceptInviteData{Title: "Accept Invitation", Token: token, Error: "Invalid form submission."})
		return
	}
	name := strings.TrimSpace(r.FormValue("display_name"))
	password := r.FormValue("password")
	if name == "" || len(password) < 12 {
		h.renderAcceptInvite(w, acceptInviteData{Title: "Accept Invitation", Token: token, Error: "Enter your name and a password of at least 12 characters."})
		return
	}
	_, err := h.admin.AcceptInvite(r.Context(), token, name, password)
	if err != nil {
		message := "Failed to accept invitation."
		switch {
		case errors.Is(err, sqlite.ErrInviteNotFound):
			message = "Invitation not found."
		case errors.Is(err, sqlite.ErrInviteConsumed):
			message = "This invitation has already been used."
		case errors.Is(err, sqlite.ErrInviteExpired):
			message = "This invitation has expired."
		}
		h.renderAcceptInvite(w, acceptInviteData{Title: "Accept Invitation", Token: token, Error: message})
		return
	}
	h.renderAcceptInvite(w, acceptInviteData{Title: "Invitation Accepted", Success: true})
}

func (h *Handler) renderAcceptInvite(w http.ResponseWriter, data acceptInviteData) {
	w.Header().Set("Cache-Control", "no-store")
	if err := h.invite.ExecuteTemplate(w, "accept-invite.html", data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}
