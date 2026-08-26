package ingest

import (
	"context"
	"fmt"
	"strings"

	"urgentry/internal/envelope"
	"urgentry/internal/middleware"
	"urgentry/internal/sqlite"
)

func persistEnvelopeSideEffects(ctx context.Context, deps IngestDeps, env *envelope.Envelope, projectID, baseEventID string) error {
	if env == nil {
		return nil
	}
	replayIndexes := map[string]struct{}{}
	replayPolicy := sqlite.DefaultReplayIngestPolicy()
	replayPolicyLoaded := false
	replayAllowed := true
	replayDropReason := ""
	activeReplayEventID := baseEventID
	activeReplaySegmentID := 0
	activeReplaySegmentKnown := false
	replayEventReceived := false
	replaySegmentsStored := 0
	replayReceiptPayload := []byte(nil)

	if hasReplayEnvelopeItems(env.Items) {
		activeReplayEventID, _ = envelopeReplayIDs(env.Items, baseEventID)
		if deps.ReplayPolicies != nil {
			policy, err := deps.ReplayPolicies.GetReplayIngestPolicy(ctx, projectID)
			if err != nil {
				return fmt.Errorf("load replay ingest policy: %w", err)
			}
			replayPolicy = policy
			replayPolicyLoaded = true
			replayAllowed = replayIncludedBySample(replayPolicy, activeReplayEventID)
			if !replayAllowed {
				saveReplayPolicyOutcome(ctx, deps.OutcomeStore, projectID, activeReplayEventID, "sample_rate", replayPolicy)
			}
		}
	}

	for idx, item := range env.Items {
		switch item.Header.Type {
		case "event", "transaction":
		case "user_report":
			saveFeedback(ctx, deps.FeedbackStore, projectID, item.Payload)
		case "attachment":
			saveAttachmentWithID(ctx, deps.AttachmentStore, deps.BlobStore, projectID, baseEventID, stableEnvelopeAttachmentID(projectID, baseEventID, item, idx), item)
		case "replay_event":
			if replayPolicyLoaded && !replayAllowed {
				continue
			}
			payload := item.Payload
			replayEventReceived = true
			if segmentID, ok := replayEventSegmentID(item.Payload); ok {
				activeReplaySegmentID = segmentID
				activeReplaySegmentKnown = true
			}
			if replayPolicyLoaded {
				replayReceiptPayload = append(replayReceiptPayload[:0], item.Payload...)
				payload = annotateReplayReceiptPayload(replayReceiptPayload, replayPolicy, replayDropReason)
			}
			replayEventID, err := saveReplayEvent(ctx, deps.ReplayStore, deps.EventStore, projectID, activeReplayEventID, payload)
			if err != nil {
				return fmt.Errorf("save replay metadata: %w", err)
			}
			if replayEventID != "" {
				activeReplayEventID = replayEventID
				replayIndexes[replayEventID] = struct{}{}
			}
		case "replay_recording", "replay_recording_not_chunked":
			if replayPolicyLoaded && !replayAllowed {
				continue
			}
			segmentID, segmentKnown := replayRecordingSegmentID(item.Payload)
			if !segmentKnown && activeReplaySegmentKnown {
				segmentID = activeReplaySegmentID
				segmentKnown = true
			}
			if !segmentKnown {
				segmentID = idx
			}
			payload := item.Payload
			if replayPolicyLoaded {
				payload = scrubReplayRecordingPayload(payload, replayPolicy)
			}
			if replayPolicyLoaded && replayDropReason == "" {
				if deps.ReplayStore == nil {
					return fmt.Errorf("save replay recording: replay store is unavailable")
				}
				projectedBytes, err := deps.ReplayStore.ProjectedReplayRecordingBytes(ctx, projectID, activeReplayEventID, segmentID, int64(len(payload)))
				if err != nil {
					return fmt.Errorf("enforce replay ingest policy: %w", err)
				}
				if projectedBytes > replayPolicy.MaxBytes {
					replayDropReason = "replay attachment exceeds max_bytes policy"
					logger := middleware.LogFromCtx(ctx)
					logger.Warn().Str("project_id", projectID).Str("replay_id", activeReplayEventID).Int64("projected_bytes", projectedBytes).Int64("max_bytes", replayPolicy.MaxBytes).Msg("envelope: replay recording dropped by ingest policy")
					saveReplayPolicyOutcome(ctx, deps.OutcomeStore, projectID, activeReplayEventID, "max_bytes", replayPolicy)
				}
			}
			if replayDropReason != "" {
				continue
			}
			if deps.ReplayStore == nil {
				return fmt.Errorf("save replay recording: replay store is unavailable")
			}
			if err := deps.ReplayStore.SaveReplayRecording(ctx, projectID, activeReplayEventID, segmentID, payload); err != nil {
				return fmt.Errorf("save replay recording: %w", err)
			}
			logger := middleware.LogFromCtx(ctx)
			replaySegmentsStored++
			logger.Info().Str("project_id", projectID).Str("replay_id", activeReplayEventID).Int("segment_id", segmentID).Int("bytes", len(payload)).Msg("envelope: replay segment stored")
			if strings.TrimSpace(activeReplayEventID) != "" {
				replayIndexes[activeReplayEventID] = struct{}{}
			}
		case "replay_video":
			if replayPolicyLoaded && !replayAllowed {
				continue
			}
			if err := saveReplayAttachment(ctx, deps.AttachmentStore, projectID, activeReplayEventID, item); err != nil {
				return fmt.Errorf("save replay video: %w", err)
			}
		case "profile":
			saveProfileEvent(ctx, deps.ProfileStore, deps.EventStore, projectID, item.Payload)
		case "client_report":
			saveClientReport(ctx, deps.OutcomeStore, projectID, baseEventID, item.Payload)
		case "session":
			saveSession(ctx, deps.SessionStore, projectID, item.Payload)
		case "sessions":
			saveSessionAggregates(ctx, deps.SessionStore, deps.AlertDeps, projectID, item.Payload)
		case "check_in":
			saveCheckIn(ctx, deps.MonitorStore, projectID, item.Payload)
		case "statsd", "metric_buckets":
			saveStatsdMetrics(ctx, deps.MetricBuckets, projectID, item.Payload)
		default:
			logger := middleware.LogFromCtx(ctx)
			logger.Warn().Str("project_id", projectID).Str("type", item.Header.Type).Msg("envelope: unknown side-effect item type, skipping")
		}
	}

	if replayPolicyLoaded && replayAllowed && replayDropReason != "" && len(replayReceiptPayload) > 0 {
		replayEventID, err := saveReplayEvent(ctx, deps.ReplayStore, deps.EventStore, projectID, activeReplayEventID, annotateReplayReceiptPayload(replayReceiptPayload, replayPolicy, replayDropReason))
		if err != nil {
			return fmt.Errorf("save replay policy outcome: %w", err)
		}
		if replayEventID != "" {
			replayIndexes[replayEventID] = struct{}{}
		}
	}
	if replayEventReceived && replayAllowed && replayDropReason == "" && replaySegmentsStored == 0 {
		logger := middleware.LogFromCtx(ctx)
		logger.Warn().Str("project_id", projectID).Str("replay_id", activeReplayEventID).Msg("envelope: replay metadata received without a replay_recording item")
	}
	if deps.ReplayStore != nil {
		for replayID := range replayIndexes {
			if err := deps.ReplayStore.IndexReplay(ctx, projectID, replayID); err != nil {
				return fmt.Errorf("index replay: %w", err)
			}
		}
	}
	return nil
}
