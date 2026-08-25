package app

import (
	"testing"

	"urgentry/internal/sqlite"
)

func TestNewHTTPDepsPassesFeedbackStoreToAPI(t *testing.T) {
	db, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer db.Close()

	feedbackStore := sqlite.NewFeedbackStore(db)
	deps := newHTTPDeps(httpDepsInput{
		db:            db,
		dataDir:       t.TempDir(),
		feedbackStore: feedbackStore,
	})

	if deps.Ingest.FeedbackStore != feedbackStore {
		t.Fatalf("ingest feedback store = %p, want %p", deps.Ingest.FeedbackStore, feedbackStore)
	}
	if deps.API.FeedbackStore != feedbackStore {
		t.Fatalf("api feedback store = %p, want %p", deps.API.FeedbackStore, feedbackStore)
	}
}

func TestNewHTTPDepsPassesCodeMappingsToAPIAndWeb(t *testing.T) {
	db, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer db.Close()

	codeMappings := sqlite.NewCodeMappingStore(db)
	deps := newHTTPDeps(httpDepsInput{
		db:           db,
		dataDir:      t.TempDir(),
		codeMappings: codeMappings,
	})

	if deps.API.CodeMappings != codeMappings {
		t.Fatalf("api code mapping store = %p, want %p", deps.API.CodeMappings, codeMappings)
	}
	if deps.Web.CodeMappings != codeMappings {
		t.Fatalf("web code mapping store = %p, want %p", deps.Web.CodeMappings, codeMappings)
	}
}
