package api

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNormalizeReplayPlayerEventsUsesReplayStartForMixedTimestamps(t *testing.T) {
	startedAt := time.UnixMilli(1787690060441).UTC()
	events := []json.RawMessage{
		json.RawMessage(`{"type":3,"timestamp":1787690062441,"data":{"source":1}}`),
		json.RawMessage(`{"type":2,"timestamp":0,"data":{"node":{}}}`),
		json.RawMessage(`{"type":3,"timestamp":500,"data":{"source":1}}`),
		json.RawMessage(`{"type":4,"timestamp":1787690060.441,"data":{"href":"https://example.com"}}`),
	}

	normalized := normalizeReplayPlayerEvents(events, startedAt)
	want := []float64{1787690060441, 1787690060441, 1787690060941, 1787690062441}
	if len(normalized) != len(want) {
		t.Fatalf("len(normalized) = %d, want %d", len(normalized), len(want))
	}
	for i := range normalized {
		got, ok := replayPlayerEventTimestamp(normalized[i])
		if !ok || got != want[i] {
			t.Fatalf("timestamp[%d] = %v, want %v", i, got, want[i])
		}
	}
}
