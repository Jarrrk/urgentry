package sqlite

import (
	"context"
	"testing"

	"urgentry/internal/store"
)

func TestCodeMappingStorePersistsProvider(t *testing.T) {
	db := openStoreTestDB(t)
	mappings := NewCodeMappingStore(db)
	mapping := &store.CodeMapping{
		ProjectID:     "project-1",
		StackRoot:     "/srv/game/",
		SourceRoot:    "resources/",
		DefaultBranch: "main",
		RepoURL:       "https://forge.hlf.is/highlife/game",
		Provider:      store.CodeMappingProviderForgejo,
	}
	if err := mappings.CreateCodeMapping(context.Background(), mapping); err != nil {
		t.Fatalf("CreateCodeMapping: %v", err)
	}

	got, err := mappings.ListCodeMappings(context.Background(), "project-1")
	if err != nil {
		t.Fatalf("ListCodeMappings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("mapping count = %d, want 1", len(got))
	}
	if got[0].Provider != store.CodeMappingProviderForgejo {
		t.Fatalf("provider = %q, want %q", got[0].Provider, store.CodeMappingProviderForgejo)
	}
}
