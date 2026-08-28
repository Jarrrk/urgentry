package web

import (
	"context"
	"fmt"
	"net/http"
)

func (h *Handler) issueProjectVersion(w http.ResponseWriter, r *http.Request) {
	if h.issueUpdates == nil {
		http.Error(w, "Live issue updates unavailable", http.StatusServiceUnavailable)
		return
	}
	writeIssueVersion(w, h.issueUpdates.ProjectVersion(r.PathValue("id")))
}

func (h *Handler) issueVersion(w http.ResponseWriter, r *http.Request) {
	if h.issueUpdates == nil {
		http.Error(w, "Live issue updates unavailable", http.StatusServiceUnavailable)
		return
	}
	writeIssueVersion(w, h.issueUpdates.IssueVersion(r.PathValue("id")))
}

func writeIssueVersion(w http.ResponseWriter, version uint64) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	_, _ = fmt.Fprint(w, version)
}

// issueLiveRefresh releases EventSource connections created by the previous
// implementation and tells already-open pages to load the polling version.
func (h *Handler) issueLiveRefresh(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	_, _ = fmt.Fprint(w, "event: changed\ndata: 1\n\n")
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
