package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func TestDashboardProfilerForcedStructuredLog(t *testing.T) {
	var output bytes.Buffer
	previous := log.Logger
	log.Logger = zerolog.New(&output)
	t.Cleanup(func() { log.Logger = previous })

	profiler := newDashboardProfiler(nil)
	step := profiler.begin()
	profiler.record("summary", step, 1, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/?dashboard_debug=1", nil)
	profiler.finish(recorder, request, pageScope{ProjectID: "project-1", ProjectSlug: "website"}, "production")

	logged := output.String()
	for _, want := range []string{`"message":"dashboard timing"`, `"project_id":"project-1"`, `"project_slug":"website"`, `"dashboard_summary_ms":`} {
		if !strings.Contains(logged, want) {
			t.Fatalf("dashboard timing log missing %q: %s", want, logged)
		}
	}
	if timing := recorder.Header().Get("Server-Timing"); !strings.Contains(timing, "summary;dur=") {
		t.Fatalf("Server-Timing missing summary duration: %s", timing)
	}
}
