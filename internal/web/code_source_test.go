package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"urgentry/internal/store"
)

func TestForgejoSourceContext(t *testing.T) {
	requests := 0
	forgejo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/v1/repos/HighLife/core/raw/[highlife]/highlife/client/core/error.lua" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("ref") != "master" {
			t.Errorf("ref = %q", r.URL.Query().Get("ref"))
		}
		if r.Header.Get("Authorization") != "token source-token" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(strings.Join([]string{
			"line 1", "line 2", "line 3", "line 4", "line 5", "line 6",
			"error line", "line 8", "line 9", "line 10", "line 11", "line 12",
		}, "\n")))
	}))
	defer forgejo.Close()

	mapping := &store.CodeMapping{
		StackRoot: "highlife/", SourceRoot: "[highlife]/highlife/", DefaultBranch: "master",
		RepoURL: forgejo.URL + "/HighLife/core", Provider: store.CodeMappingProviderForgejo,
	}
	groups := []exceptionGroup{{Frames: []richFrame{{File: "highlife/client/core/error.lua", LineNo: 7}}}}
	frames := []stackFrame{{File: "highlife/client/core/error.lua", LineNo: 7}}
	handler := &Handler{codeSource: newForgejoSourceClient(forgejo.URL, "source-token", forgejo.Client())}
	handler.applyCodeSourceContext(t.Context(), groups, frames, []*store.CodeMapping{mapping})

	if requests != 1 {
		t.Fatalf("source requests = %d, want 1", requests)
	}
	if !groups[0].Frames[0].HasContext || groups[0].Frames[0].ContextLine != "error line" {
		t.Fatalf("rich source context = %+v", groups[0].Frames[0])
	}
	if len(frames[0].CodeLines) != 11 || !frames[0].CodeLines[5].Highlight || frames[0].CodeLines[5].Content != "error line" {
		t.Fatalf("flat source context = %+v", frames[0].CodeLines)
	}
}
