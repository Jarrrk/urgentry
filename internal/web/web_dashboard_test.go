package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"urgentry/internal/auth"
	"urgentry/internal/sqlite"
	"urgentry/internal/store"
	profilefixtures "urgentry/internal/testfixtures/profiles"
)

func TestDashboardPage(t *testing.T) {
	srv, _ := setupTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body := getBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "Dashboard") {
		t.Error("expected body to contain 'Dashboard'")
	}
	if !strings.Contains(body, "Events") && !strings.Contains(body, "events") {
		t.Error("expected body to contain event metrics")
	}
}

func TestDashboardPageHonorsSelectedProject(t *testing.T) {
	srv, db := setupTestServer(t)
	defer srv.Close()

	insertGroup(t, db, "grp-overview-default", "Default overview issue", "default.go", "error", "unresolved")
	insertEvent(t, db, "evt-overview-default", "grp-overview-default", "Default overview event", "error", "default failure")
	if _, err := db.Exec(`INSERT INTO projects (id, organization_id, slug, name, platform, status)
		VALUES ('mobile-proj', 'test-org', 'mobile-app', 'Mobile App', 'javascript', 'active')`); err != nil {
		t.Fatalf("insert selected project: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO groups
		(id, project_id, grouping_version, grouping_key, title, culprit, level, status, first_seen, last_seen, times_seen, short_id)
		VALUES ('grp-overview-mobile', 'mobile-proj', 'urgentry-v1', 'grp-overview-mobile', 'Selected overview issue', 'mobile.js', 'error', 'unresolved', ?, ?, 1, 2)`, now, now); err != nil {
		t.Fatalf("insert selected project issue: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO events
		(id, project_id, event_id, group_id, level, event_type, title, message, platform, culprit, occurred_at, ingested_at, tags_json, payload_json, user_identifier)
		VALUES ('evt-overview-mobile-internal', 'mobile-proj', 'evt-overview-mobile', 'grp-overview-mobile', 'error', 'error', 'Selected overview event', 'selected failure', 'javascript', 'mobile.js', ?, ?, '{}', '{}', 'mobile-user')`, now, now); err != nil {
		t.Fatalf("insert selected project event: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO events
		(id, project_id, event_id, group_id, level, event_type, title, message, platform, culprit, occurred_at, ingested_at, tags_json, payload_json) VALUES
		('evt-overview-default-log-internal', 'test-proj', 'evt-overview-default-log', 'grp-overview-default', 'info', 'log', 'Default overview log', 'default log', 'go', 'default.go', ?, ?, '{}', '{"logger":"default.logger"}'),
		('evt-overview-mobile-log-internal', 'mobile-proj', 'evt-overview-mobile-log', 'grp-overview-mobile', 'info', 'log', 'Selected overview log', 'selected log', 'javascript', 'mobile.js', ?, ?, '{}', '{"logger":"selected.logger"}')`, now, now, now, now); err != nil {
		t.Fatalf("insert overview logs: %v", err)
	}
	traces := sqlite.NewTraceStore(db)
	for _, transaction := range []store.StoredTransaction{
		{ProjectID: "test-proj", EventID: "txn-overview-default", TraceID: "trace-overview-default", SpanID: "span-overview-default", Transaction: "default-overview-transaction", Op: "http.server", Status: "ok", StartTimestamp: time.Now().UTC().Add(-200 * time.Millisecond), EndTimestamp: time.Now().UTC(), DurationMS: 200},
		{ProjectID: "mobile-proj", EventID: "txn-overview-mobile", TraceID: "trace-overview-mobile", SpanID: "span-overview-mobile", Transaction: "selected-overview-transaction", Op: "http.server", Status: "ok", StartTimestamp: time.Now().UTC().Add(-100 * time.Millisecond), EndTimestamp: time.Now().UTC(), DurationMS: 100},
	} {
		if err := traces.SaveTransaction(t.Context(), &transaction); err != nil {
			t.Fatalf("insert overview transaction: %v", err)
		}
	}

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: selectedProjectCookie, Value: "test-org%2Fmobile-app"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body := getBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, body)
	}
	for _, want := range []string{"Selected overview issue", "Selected overview event", "Selected overview log", "selected-overview-transaction"} {
		if !strings.Contains(body, want) {
			t.Fatalf("selected project dashboard content missing %q: %s", want, body)
		}
	}
	for _, unwanted := range []string{"Default overview issue", "Default overview event", "Default overview log", "default-overview-transaction"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("default project dashboard content leaked into selected project: %s", body)
		}
	}
}

func TestDashboardSavedQueryWidgets(t *testing.T) {
	srv, db := setupTestServer(t)
	defer srv.Close()

	insertGroup(t, db, "grp-widget-1", "ValueError: bad input", "main.go", "error", "unresolved")
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO saved_searches (id, user_id, name, query, filter, environment, sort, created_at)
		 VALUES ('saved-widget-1', 'user-1', 'Hot issues', 'ValueError', 'all', '', 'last_seen', ?)`,
		now,
	); err != nil {
		t.Fatalf("insert saved search: %v", err)
	}

	blobs := store.NewMemoryBlobStore()
	handler := NewHandlerWithDeps(testHandlerDeps(db, blobs, t.TempDir(), nil))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), &auth.Principal{User: &auth.User{ID: "user-1"}}))
	rr := httptest.NewRecorder()
	handler.dashboardPage(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Saved Query Widgets") {
		t.Fatalf("expected dashboard widgets section, got %s", body)
	}
	if !strings.Contains(body, "Hot issues") || !strings.Contains(body, "ValueError") {
		t.Fatalf("expected saved query widget content, got %s", body)
	}
}

func TestDashboardAnalyticsHomePage(t *testing.T) {
	srv, db, sessionToken, _ := setupAuthorizedTestServer(t)
	defer srv.Close()

	insertGroup(t, db, "grp-home-1", "CheckoutError", "checkout.go", "error", "unresolved")
	insertEvent(t, db, "evt-home-1", "grp-home-1", "CheckoutError", "error", "boom")

	now := time.Now().UTC()
	if _, err := db.Exec(
		`INSERT INTO events
			(id, project_id, event_id, group_id, release, environment, platform, level, event_type, title, message, culprit, occurred_at, ingested_at, payload_json, tags_json)
		 VALUES
			('evt-home-log-1', 'test-proj', 'evt-home-log-1', 'grp-home-1', 'backend@1.2.3', 'production', 'go', 'warning', 'log', 'Payment retry', 'worker saturated', 'checkout.go', ?, ?, '{"logger":"auth.checkout","contexts":{"trace":{"trace_id":"trace-home-1"}}}', '{"environment":"production"}')`,
		now.Format(time.RFC3339),
		now.Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert dashboard log event: %v", err)
	}

	traces := sqlite.NewTraceStore(db)
	if err := traces.SaveTransaction(t.Context(), &store.StoredTransaction{
		ProjectID:      "test-proj",
		EventID:        "txn-home-1",
		TraceID:        "trace-home-1",
		SpanID:         "span-home-1",
		Transaction:    "checkout",
		Op:             "http.server",
		Status:         "ok",
		Environment:    "production",
		ReleaseID:      "backend@1.2.3",
		StartTimestamp: now.Add(-250 * time.Millisecond),
		EndTimestamp:   now,
		DurationMS:     250,
	}); err != nil {
		t.Fatalf("SaveTransaction: %v", err)
	}

	releaseStore := sqlite.NewReleaseStore(db)
	if _, err := releaseStore.CreateRelease(t.Context(), "test-org", "backend@1.2.3"); err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}
	health := sqlite.NewReleaseHealthStore(db)
	if err := health.SaveSession(t.Context(), &sqlite.ReleaseSession{
		ProjectID:   "test-proj",
		Release:     "backend@1.2.3",
		Status:      "ok",
		Quantity:    5,
		DistinctID:  "user-1",
		DateCreated: now,
	}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	blobStore := store.NewMemoryBlobStore()
	replays := sqlite.NewReplayStore(db, blobStore)
	if _, err := replays.SaveEnvelopeReplay(t.Context(), "test-proj", "evt-home-replay-1", []byte(`{
		"event_id":"evt-home-replay-1",
		"replay_id":"replay-home-1",
		"timestamp":"2026-03-29T12:00:00Z",
		"platform":"javascript",
		"release":"web@1.2.3",
		"environment":"production",
		"request":{"url":"https://app.example.com/checkout"},
		"user":{"email":"owner@example.com"},
		"contexts":{"trace":{"trace_id":"trace-home-1"}}
	}`)); err != nil {
		t.Fatalf("SaveEnvelopeReplay: %v", err)
	}
	if err := replays.IndexReplay(t.Context(), "test-proj", "replay-home-1"); err != nil {
		t.Fatalf("IndexReplay: %v", err)
	}

	profiles := sqlite.NewProfileStore(db, blobStore)
	profilefixtures.Save(t, profiles, "test-proj", profilefixtures.DBHeavy().Spec().
		WithIDs("evt-home-profile-1", "profile-home-1").
		WithTransaction("checkout").
		WithTrace("trace-home-1").
		WithRelease("backend@1.2.3").
		WithEnvironment("production"))

	client := &http.Client{}
	resp := sessionRequest(t, client, http.MethodGet, srv.URL+"/", sessionToken, "", "", nil)
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	for _, snippet := range []string{
		"Analytics home ties the live surfaces together.",
		"Starter Views",
		"Slow endpoints",
		"Recent Transactions",
		"checkout",
		"Recent Logs",
		"auth.checkout",
		"Recent Releases",
		"backend@1.2.3",
		"Recent Replays",
		"Replay of https://app.example.com/checkout",
		"Recent Profiles",
		"Profile for checkout",
		"Issue watchlist",
		"Recent latency",
		"250 ms",
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("dashboard body missing %q: %s", snippet, body)
		}
	}
}

func TestDashboardStarterTemplates(t *testing.T) {
	srv, db, sessionToken, csrf := setupAuthorizedTestServer(t)
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}

	resp := sessionRequest(t, client, http.MethodGet, srv.URL+"/dashboards/", sessionToken, "", "", nil)
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dashboards list status = %d, want 200", resp.StatusCode)
	}
	for _, snippet := range []string{
		"Starter Packs",
		"Ops triage starter",
		"Release watch",
		"Performance pulse",
		`aria-label="Create starter dashboard: Ops triage starter"`,
		`aria-label="Create starter dashboard: Release watch"`,
		`aria-label="Create starter dashboard: Performance pulse"`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected dashboards page to contain %q: %s", snippet, body)
		}
	}

	resp = sessionRequest(t, client, http.MethodPost, srv.URL+"/dashboards/starter/ops-triage/create", sessionToken, csrf, "application/x-www-form-urlencoded", strings.NewReader(""))
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("starter dashboard status = %d, want 303", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	if !strings.HasPrefix(location, "/dashboards/") {
		t.Fatalf("unexpected starter dashboard redirect %q", location)
	}

	resp = sessionRequest(t, client, http.MethodGet, srv.URL+location, sessionToken, "", "", nil)
	body = getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("starter dashboard detail status = %d, want 200", resp.StatusCode)
	}
	for _, snippet := range []string{
		"Ops triage starter",
		"Unresolved issues",
		"Production issues by release",
		"Recent api logs",
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("expected starter dashboard body to contain %q: %s", snippet, body)
		}
	}

	var widgetCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM dashboard_widgets`).Scan(&widgetCount); err != nil {
		t.Fatalf("count starter widgets: %v", err)
	}
	if widgetCount != 3 {
		t.Fatalf("starter widget count = %d, want 3", widgetCount)
	}
}
