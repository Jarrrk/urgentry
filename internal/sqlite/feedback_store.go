package sqlite

import (
	"context"
	"database/sql"
	"time"
)

// Feedback represents a user feedback entry from the user_feedback table.
type Feedback struct {
	ID        string
	ProjectID string
	EventID   string
	GroupID   string
	Name      string
	Email     string
	Comments  string
	CreatedAt time.Time
}

// FeedbackStore persists user feedback (crash reports) to SQLite.
type FeedbackStore struct {
	db *sql.DB
}

// NewFeedbackStore creates a FeedbackStore backed by the given database.
func NewFeedbackStore(db *sql.DB) *FeedbackStore {
	return &FeedbackStore{db: db}
}

// SaveFeedback persists a user feedback entry.
func (s *FeedbackStore) SaveFeedback(ctx context.Context, projectID, eventID, name, email, comments string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var groupID sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT group_id FROM events
		 WHERE project_id = ? AND event_id = ?
		 ORDER BY ingested_at DESC LIMIT 1`, projectID, eventID).Scan(&groupID); err != nil && err != sql.ErrNoRows {
		return err
	}

	result, err := tx.ExecContext(ctx,
		`UPDATE user_feedback
		 SET name = ?, email = ?, comments = ?, group_id = COALESCE(?, group_id), created_at = datetime('now')
		 WHERE project_id = ? AND event_id = ?`,
		name, email, comments, groupID, projectID, eventID)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated == 0 {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO user_feedback (id, project_id, event_id, group_id, name, email, comments)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			generateID(), projectID, eventID, groupID, name, email, comments)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListFeedback returns recent feedback entries for a project.
func (s *FeedbackStore) ListFeedback(ctx context.Context, projectID string, limit int) ([]Feedback, error) {
	if limit <= 0 {
		limit = 25
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, event_id, group_id, name, email, comments, created_at
		 FROM user_feedback WHERE project_id = ?
		 ORDER BY created_at DESC LIMIT ?`,
		projectID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var feedback []Feedback
	for rows.Next() {
		var f Feedback
		var eventID, groupID, name, email, comments, createdAt sql.NullString
		if err := rows.Scan(&f.ID, &f.ProjectID, &eventID, &groupID,
			&name, &email, &comments, &createdAt); err != nil {
			return nil, err
		}
		f.EventID = nullStr(eventID)
		f.GroupID = nullStr(groupID)
		f.Name = nullStr(name)
		f.Email = nullStr(email)
		f.Comments = nullStr(comments)
		f.CreatedAt = parseTime(nullStr(createdAt))
		feedback = append(feedback, f)
	}
	return feedback, rows.Err()
}
