package web

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"urgentry/internal/middleware"
)

const slowDashboardThreshold = time.Second

type dashboardStepStart struct {
	at           time.Time
	waitCount    int64
	waitDuration time.Duration
}

type dashboardStepTiming struct {
	name         string
	duration     time.Duration
	waitCount    int64
	waitDuration time.Duration
	rows         int
	err          error
}

type dashboardProfiler struct {
	db      *sql.DB
	started time.Time
	steps   []dashboardStepTiming
}

func newDashboardProfiler(db *sql.DB) *dashboardProfiler {
	return &dashboardProfiler{db: db, started: time.Now()}
}

func (p *dashboardProfiler) begin() dashboardStepStart {
	start := dashboardStepStart{at: time.Now()}
	if p.db != nil {
		stats := p.db.Stats()
		start.waitCount = stats.WaitCount
		start.waitDuration = stats.WaitDuration
	}
	return start
}

func (p *dashboardProfiler) record(name string, start dashboardStepStart, rows int, err error) {
	step := dashboardStepTiming{name: name, duration: time.Since(start.at), rows: rows, err: err}
	if p.db != nil {
		stats := p.db.Stats()
		step.waitCount = stats.WaitCount - start.waitCount
		step.waitDuration = stats.WaitDuration - start.waitDuration
	}
	p.steps = append(p.steps, step)
}

func (p *dashboardProfiler) finish(w http.ResponseWriter, r *http.Request, scope pageScope, environment string) {
	total := time.Since(p.started)
	serverTiming := make([]string, 0, len(p.steps)+1)
	serverTiming = append(serverTiming, fmt.Sprintf("dashboard;dur=%.3f", durationMS(total)))

	logger := middleware.LogFromCtx(r.Context())
	event := logger.Debug()
	forced := r.URL.Query().Get("dashboard_debug") == "1" || r.Header.Get("X-Urgentry-Dashboard-Debug") == "1"
	if forced {
		event = logger.Info()
	}
	if total >= slowDashboardThreshold {
		event = logger.Warn()
	}
	event = event.
		Str("project_id", scope.ProjectID).
		Str("project_slug", scope.ProjectSlug).
		Str("environment", environment).
		Float64("dashboard_total_ms", durationMS(total))

	var totalWaitCount int64
	var totalWaitDuration time.Duration
	for _, step := range p.steps {
		serverTiming = append(serverTiming, fmt.Sprintf("%s;dur=%.3f", step.name, durationMS(step.duration)))
		event = event.Float64("dashboard_"+step.name+"_ms", durationMS(step.duration))
		if step.rows >= 0 {
			event = event.Int("dashboard_"+step.name+"_rows", step.rows)
		}
		if step.waitCount > 0 {
			event = event.Int64("dashboard_"+step.name+"_db_wait_count", step.waitCount)
		}
		if step.waitDuration > 0 {
			event = event.Float64("dashboard_"+step.name+"_db_wait_ms", durationMS(step.waitDuration))
		}
		if step.err != nil {
			event = event.Str("dashboard_"+step.name+"_error", step.err.Error())
		}
		totalWaitCount += step.waitCount
		totalWaitDuration += step.waitDuration
	}
	if p.db != nil {
		stats := p.db.Stats()
		event = event.
			Int64("dashboard_db_wait_count", totalWaitCount).
			Float64("dashboard_db_wait_ms", durationMS(totalWaitDuration)).
			Int("dashboard_db_open_connections", stats.OpenConnections).
			Int("dashboard_db_in_use", stats.InUse)
	}
	w.Header().Set("Server-Timing", strings.Join(serverTiming, ", "))
	event.Msg("dashboard timing")
}

func durationMS(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
