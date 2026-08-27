package web

import (
	"context"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------
// Feedback Page
// ---------------------------------------------------------------------------

type feedbackData struct {
	Title        string
	Nav          string
	Environment  string   // selected environment ("" = all)
	Environments []string // available environments for global nav
	Feedback     []feedbackRow
}

type feedbackRow struct {
	ID        string
	Name      string
	Email     string
	Comments  string
	EventID   string
	GroupID   string
	CreatedAt string
}

func (h *Handler) feedbackPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	scope, err := h.defaultPageScope(ctx)
	if err != nil {
		writeWebInternal(w, r, "Failed to resolve selected project.")
		return
	}

	var items []feedbackRow
	if h.webStore != nil {
		rows, err := h.listFeedbackDB(ctx, scope.ProjectID, 100)
		if err != nil {
			http.Error(w, "Failed to load feedback.", http.StatusInternalServerError)
			return
		}
		items = rows
	}

	data := feedbackData{
		Title:        "User Feedback",
		Nav:          "feedback",
		Environment:  readSelectedEnvironment(r),
		Environments: h.loadEnvironments(ctx),
		Feedback:     items,
	}

	h.render(w, "feedback.html", data)
}

// ---------------------------------------------------------------------------
// Feedback Detail Page
// ---------------------------------------------------------------------------

type feedbackDetailData struct {
	Title        string
	Nav          string
	Environment  string
	Environments []string
	Feedback     feedbackRow
	ReplayID     string
}

func (h *Handler) feedbackDetailPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing feedback id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	scope, err := h.defaultPageScope(ctx)
	if err != nil {
		writeWebInternal(w, r, "Failed to resolve selected project.")
		return
	}
	row, err := h.webStore.GetFeedback(ctx, scope.ProjectID, id)
	if err != nil {
		http.Error(w, "Failed to load feedback.", http.StatusInternalServerError)
		return
	}
	if row == nil {
		http.Error(w, "Feedback not found.", http.StatusNotFound)
		return
	}

	data := feedbackDetailData{
		Title:        "Feedback from " + row.Name,
		Nav:          "feedback",
		Environment:  readSelectedEnvironment(r),
		Environments: h.loadEnvironments(ctx),
		Feedback: feedbackRow{
			ID:        row.ID,
			Name:      row.Name,
			Email:     row.Email,
			Comments:  row.Comments,
			EventID:   row.EventID,
			GroupID:   row.GroupID,
			CreatedAt: formatDBTime(row.CreatedAt.Format(time.RFC3339)),
		},
		ReplayID: "", // replay linkage resolved when event-level replay_id is available
	}

	h.render(w, "feedback-detail.html", data)
}

func (h *Handler) listFeedbackDB(ctx context.Context, projectID string, limit int) ([]feedbackRow, error) {
	rows, err := h.webStore.ListFeedback(ctx, projectID, limit)
	if err != nil {
		return nil, err
	}
	items := make([]feedbackRow, 0, len(rows))
	for _, row := range rows {
		items = append(items, feedbackRow{
			ID:        row.ID,
			Name:      row.Name,
			Email:     row.Email,
			Comments:  row.Comments,
			EventID:   row.EventID,
			GroupID:   row.GroupID,
			CreatedAt: formatDBTime(row.CreatedAt.Format(time.RFC3339)),
		})
	}
	return items, nil
}
