package web

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"urgentry/internal/issueupdates"
	"urgentry/internal/sqlite"
	"urgentry/internal/store"
)

func TestIssueDetailUpdatesStreamsPublishedChange(t *testing.T) {
	dataDir := t.TempDir()
	db, err := sqlite.Open(dataDir)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	insertGroup(t, db, "grp-live-1", "LiveError", "main.go", "error", "unresolved")

	broker := issueupdates.NewBroker()
	deps := testHandlerDeps(db, store.NewMemoryBlobStore(), dataDir, nil)
	deps.IssueUpdates = broker
	handler := NewHandlerWithDeps(deps)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/issues/grp-live-1/live")
	if err != nil {
		t.Fatalf("GET live updates: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("live updates status = %d, want 200", resp.StatusCode)
	}
	if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", contentType)
	}

	reader := bufio.NewReader(resp.Body)
	if line, err := reader.ReadString('\n'); err != nil || line != ": connected\n" {
		t.Fatalf("connected line = %q, err = %v", line, err)
	}
	_, _ = reader.ReadString('\n')
	broker.Publish("test-proj", "grp-live-1")

	readResult := make(chan string, 1)
	go func() {
		line, _ := reader.ReadString('\n')
		readResult <- line
	}()
	select {
	case line := <-readResult:
		if line != "event: changed\n" {
			t.Fatalf("event line = %q", line)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for issue change event")
	}
}
