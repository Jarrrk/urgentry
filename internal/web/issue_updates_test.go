package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"urgentry/internal/issueupdates"
	"urgentry/internal/sqlite"
	"urgentry/internal/store"
)

func TestIssueVersionEndpointReturnsImmediatelyAfterPublishedChange(t *testing.T) {
	dataDir := t.TempDir()
	db, err := sqlite.Open(dataDir)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	broker := issueupdates.NewBroker()
	broker.Publish("test-proj", "grp-live-1")
	deps := testHandlerDeps(db, store.NewMemoryBlobStore(), dataDir, nil)
	deps.IssueUpdates = broker
	handler := NewHandlerWithDeps(deps)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/issue-updates/issues/grp-live-1")
	if err != nil {
		t.Fatalf("GET live updates: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("live updates status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read version response: %v", err)
	}
	if string(body) != "1" {
		t.Fatalf("version response = %q, want 1", body)
	}
}
