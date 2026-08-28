package web

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

func (h *Handler) issueListUpdates(w http.ResponseWriter, r *http.Request) {
	if h.issueUpdates == nil {
		http.Error(w, "Live issue updates unavailable", http.StatusServiceUnavailable)
		return
	}
	scope, err := h.defaultPageScope(r.Context())
	if err != nil {
		http.Error(w, "Failed to resolve selected project.", http.StatusInternalServerError)
		return
	}
	if scope.ProjectID == "" {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}
	updates, unsubscribe := h.issueUpdates.SubscribeProject(scope.ProjectID)
	defer unsubscribe()
	h.streamIssueUpdates(w, r, updates)
}

func (h *Handler) issueDetailUpdates(w http.ResponseWriter, r *http.Request) {
	if h.issueUpdates == nil {
		http.Error(w, "Live issue updates unavailable", http.StatusServiceUnavailable)
		return
	}
	issueID := r.PathValue("id")
	issue, err := h.webStore.GetIssue(r.Context(), issueID)
	if err != nil {
		http.Error(w, "Failed to load issue.", http.StatusInternalServerError)
		return
	}
	if issue == nil {
		http.NotFound(w, r)
		return
	}
	updates, unsubscribe := h.issueUpdates.SubscribeIssue(issueID)
	defer unsubscribe()
	h.streamIssueUpdates(w, r, updates)
}

func (h *Handler) streamIssueUpdates(w http.ResponseWriter, r *http.Request, updates <-chan struct{}) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-updates:
			if _, err := fmt.Fprint(w, "event: changed\ndata: 1\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (h *Handler) publishIssueUpdate(ctx context.Context, issueID string) {
	if h.issueUpdates == nil || h.db == nil || issueID == "" {
		return
	}
	var projectID string
	if err := h.db.QueryRowContext(ctx, `SELECT project_id FROM groups WHERE id = ?`, issueID).Scan(&projectID); err == nil {
		h.issueUpdates.Publish(projectID, issueID)
	}
}
