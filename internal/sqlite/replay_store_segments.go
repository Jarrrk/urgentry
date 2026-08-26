package sqlite

import (
	"context"
	"crypto/sha1"
	"fmt"
	"strings"
	"time"
)

func (s *ReplayStore) ProjectedReplayRecordingBytes(ctx context.Context, projectID, replayID string, segmentID int, payloadSize int64) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("replay store is unavailable")
	}
	var total, existing int64
	err := s.db.QueryRowContext(ctx,
		`SELECT
			COALESCE(SUM(size_bytes), 0),
			COALESCE(MAX(CASE WHEN segment_id = ? THEN size_bytes ELSE 0 END), 0)
		 FROM replay_segments
		 WHERE project_id = ? AND replay_id = ?`,
		segmentID, strings.TrimSpace(projectID), strings.TrimSpace(replayID),
	).Scan(&total, &existing)
	if err != nil {
		return 0, fmt.Errorf("measure replay recording: %w", err)
	}
	return total - existing + payloadSize, nil
}

func (s *ReplayStore) SaveReplayRecording(ctx context.Context, projectID, replayID string, segmentID int, payload []byte) error {
	projectID = strings.TrimSpace(projectID)
	replayID = strings.TrimSpace(replayID)
	if s == nil || s.db == nil || s.blobs == nil {
		return fmt.Errorf("replay segment storage is unavailable")
	}
	if projectID == "" || replayID == "" || segmentID < 0 || len(payload) == 0 {
		return fmt.Errorf("invalid replay segment")
	}
	recordID := replaySegmentRecordID(projectID, replayID, segmentID)
	objectKey := fmt.Sprintf("replays/%s/%s/segment-%06d.rrweb", sanitizeKeySegment(projectID), sanitizeKeySegment(replayID), segmentID)
	if err := s.blobs.Put(ctx, objectKey, payload); err != nil {
		return fmt.Errorf("store replay segment blob: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO replay_segments
			(id, project_id, replay_id, segment_id, size_bytes, object_key, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(project_id, replay_id, segment_id) DO UPDATE SET
			size_bytes = excluded.size_bytes,
			object_key = excluded.object_key,
			updated_at = excluded.updated_at`,
		recordID, projectID, replayID, segmentID, len(payload), objectKey, now, now,
	)
	if err != nil {
		_ = s.blobs.Delete(ctx, objectKey)
		return fmt.Errorf("save replay segment metadata: %w", err)
	}
	return nil
}

func replaySegmentRecordID(projectID, replayID string, segmentID int) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%s\x00%s\x00%d", projectID, replayID, segmentID)))
	return fmt.Sprintf("rseg-%x", sum[:10])
}
