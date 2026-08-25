package web

import (
	"database/sql"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"urgentry/internal/auth"
)

func TestManagePagesRequireAuthAndRender(t *testing.T) {
	srv, _, sessionToken, csrf := setupAuthorizedTestServerWithDeps(t, func(_ *sql.DB, _ *auth.Authorizer, _ string, deps Dependencies) Dependencies {
		return deps
	})
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}

	pages := []struct {
		path    string
		contain string
	}{
		{"/manage/", "Admin Console"},
		{"/manage/organizations/", "Organizations"},
		{"/manage/projects/", "Projects"},
		{"/manage/users/", "Users"},
		{"/manage/settings/", "Retention Settings"},
		{"/manage/status/", "Go Version"},
	}

	for _, pg := range pages {
		pg := pg
		t.Run(pg.path, func(t *testing.T) {
			// Unauthenticated → redirect.
			resp := sessionRequest(t, client, http.MethodGet, srv.URL+pg.path, "", "", "", nil)
			if resp.StatusCode != http.StatusSeeOther {
				t.Fatalf("unauthenticated status = %d, want 303", resp.StatusCode)
			}
			resp.Body.Close()

			// Authenticated → 200 with expected content.
			resp = sessionRequest(t, client, http.MethodGet, srv.URL+pg.path, sessionToken, csrf, "", nil)
			body := getBody(t, resp)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("authenticated status = %d, want 200; body: %s", resp.StatusCode, body)
			}
			if !strings.Contains(body, pg.contain) {
				t.Fatalf("page %s: expected %q in body", pg.path, pg.contain)
			}
		})
	}
}

func TestAdminAliasRedirectsToManage(t *testing.T) {
	srv, _, sessionToken, csrf := setupAuthorizedTestServerWithDeps(t, func(_ *sql.DB, _ *auth.Authorizer, _ string, deps Dependencies) Dependencies {
		return deps
	})
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}

	resp := sessionRequest(t, client, http.MethodGet, srv.URL+"/admin/", "", "", "", nil)
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/login/?next=/admin/") {
		t.Fatalf("unauthenticated alias status=%d location=%q", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp.Body.Close()

	resp = sessionRequest(t, client, http.MethodGet, srv.URL+"/admin/", sessionToken, csrf, "", nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/manage/" {
		t.Fatalf("authenticated alias status=%d location=%q", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp.Body.Close()
}

func TestManageDashboardShowsCounts(t *testing.T) {
	srv, _, sessionToken, csrf := setupAuthorizedTestServerWithDeps(t, func(_ *sql.DB, _ *auth.Authorizer, _ string, deps Dependencies) Dependencies {
		return deps
	})
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp := sessionRequest(t, client, http.MethodGet, srv.URL+"/manage/", sessionToken, csrf, "", nil)
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{"Organizations", "Projects", "Users", "Database Size", "Uptime"} {
		if !strings.Contains(body, want) {
			t.Errorf("manage dashboard: missing %q", want)
		}
	}
}

func TestManageStatusShowsGoVersion(t *testing.T) {
	srv, _, sessionToken, csrf := setupAuthorizedTestServerWithDeps(t, func(_ *sql.DB, _ *auth.Authorizer, _ string, deps Dependencies) Dependencies {
		return deps
	})
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp := sessionRequest(t, client, http.MethodGet, srv.URL+"/manage/status/", sessionToken, csrf, "", nil)
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "go") {
		t.Errorf("manage status: expected go version in body")
	}
	if !strings.Contains(body, "Database") {
		t.Errorf("manage status: expected Database section in body")
	}
}

func TestManageUsersListsBootstrapUser(t *testing.T) {
	srv, _, sessionToken, csrf := setupAuthorizedTestServerWithDeps(t, func(_ *sql.DB, _ *auth.Authorizer, _ string, deps Dependencies) Dependencies {
		return deps
	})
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp := sessionRequest(t, client, http.MethodGet, srv.URL+"/manage/users/", sessionToken, csrf, "", nil)
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "owner@example.com") {
		t.Errorf("manage users: expected bootstrap user email in body")
	}
}

func TestManageOrganizationsListsOrg(t *testing.T) {
	srv, _, sessionToken, csrf := setupAuthorizedTestServerWithDeps(t, func(_ *sql.DB, _ *auth.Authorizer, _ string, deps Dependencies) Dependencies {
		return deps
	})
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp := sessionRequest(t, client, http.MethodGet, srv.URL+"/manage/organizations/", sessionToken, csrf, "", nil)
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "test-org") {
		t.Errorf("manage organizations: expected 'test-org' in body")
	}
}

func TestManageOrganizationsCreatesAndUpdatesOrganization(t *testing.T) {
	srv, db, sessionToken, csrf := setupAuthorizedTestServerWithDeps(t, func(_ *sql.DB, _ *auth.Authorizer, _ string, deps Dependencies) Dependencies {
		return deps
	})
	defer srv.Close()

	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	createForm := url.Values{"name": {"HighLife"}, "slug": {"highlife"}}
	resp := sessionRequest(t, client, http.MethodPost, srv.URL+"/manage/organizations/", sessionToken, csrf, "application/x-www-form-urlencoded", strings.NewReader(createForm.Encode()))
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create organization status = %d; body: %s", resp.StatusCode, getBody(t, resp))
	}
	resp.Body.Close()

	var ownerCount, teamCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM organization_members WHERE organization_id = (SELECT id FROM organizations WHERE slug = 'highlife') AND role = 'owner'`).Scan(&ownerCount); err != nil {
		t.Fatalf("owner count: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM teams WHERE organization_id = (SELECT id FROM organizations WHERE slug = 'highlife') AND slug = 'default'`).Scan(&teamCount); err != nil {
		t.Fatalf("team count: %v", err)
	}
	if ownerCount != 1 || teamCount != 1 {
		t.Fatalf("created organization owner=%d team=%d, want 1/1", ownerCount, teamCount)
	}

	updateForm := url.Values{"name": {"HighLife RP"}, "slug": {"highlife-rp"}}
	resp = sessionRequest(t, client, http.MethodPost, srv.URL+"/manage/organizations/highlife/update", sessionToken, csrf, "application/x-www-form-urlencoded", strings.NewReader(updateForm.Encode()))
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("update organization status = %d; body: %s", resp.StatusCode, getBody(t, resp))
	}
	resp.Body.Close()
	var name string
	if err := db.QueryRow(`SELECT name FROM organizations WHERE slug = 'highlife-rp'`).Scan(&name); err != nil || name != "HighLife RP" {
		t.Fatalf("updated organization name=%q err=%v", name, err)
	}
}

func TestManageMutationsRequireCSRF(t *testing.T) {
	srv, _, sessionToken, _ := setupAuthorizedTestServerWithDeps(t, func(_ *sql.DB, _ *auth.Authorizer, _ string, deps Dependencies) Dependencies {
		return deps
	})
	defer srv.Close()

	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	for _, target := range []string{
		"/manage/organizations/",
		"/manage/organizations/test-org/update",
		"/manage/projects/test-org/test-project/update",
		"/manage/projects/test-org/test-project/delete",
		"/manage/users/invite",
	} {
		resp := sessionRequest(t, client, http.MethodPost, srv.URL+target, sessionToken, "", "application/x-www-form-urlencoded", strings.NewReader(""))
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("POST %s without CSRF status = %d, want 403", target, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestManageUsersInvitesAndAcceptsUser(t *testing.T) {
	srv, db, sessionToken, csrf := setupAuthorizedTestServerWithDeps(t, func(db *sql.DB, _ *auth.Authorizer, _ string, deps Dependencies) Dependencies {
		if _, err := db.Exec(`INSERT INTO teams (id, organization_id, slug, name) VALUES ('team-1', 'test-org', 'backend', 'Backend')`); err != nil {
			t.Fatalf("seed team: %v", err)
		}
		return deps
	})
	defer srv.Close()

	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	form := url.Values{"email": {"dev@example.com"}, "organization": {"test-org"}, "team": {"test-org/backend"}, "role": {"admin"}}
	resp := sessionRequest(t, client, http.MethodPost, srv.URL+"/manage/users/invite", sessionToken, csrf, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create invite status = %d; body: %s", resp.StatusCode, body)
	}
	match := regexp.MustCompile(`/accept-invite/([^/]+)/`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("invite response did not contain acceptance link: %s", body)
	}

	acceptForm := url.Values{"display_name": {"Developer"}, "password": {"a-secure-password"}}
	resp = sessionRequest(t, client, http.MethodPost, srv.URL+"/accept-invite/"+match[1]+"/", "", "", "application/x-www-form-urlencoded", strings.NewReader(acceptForm.Encode()))
	body = getBody(t, resp)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Invitation accepted") {
		t.Fatalf("accept invite status = %d; body: %s", resp.StatusCode, body)
	}
	var role string
	if err := db.QueryRow(`SELECT m.role FROM organization_members m JOIN users u ON u.id = m.user_id WHERE u.email = 'dev@example.com'`).Scan(&role); err != nil || role != "admin" {
		t.Fatalf("accepted member role=%q err=%v", role, err)
	}
}

func TestManageProjectsCreatesProjectWithDefaultKey(t *testing.T) {
	srv, db, sessionToken, csrf := setupAuthorizedTestServerWithDeps(t, func(db *sql.DB, _ *auth.Authorizer, _ string, deps Dependencies) Dependencies {
		if _, err := db.Exec(`INSERT INTO teams (id, organization_id, slug, name) VALUES ('team-1', 'test-org', 'backend', 'Backend')`); err != nil {
			t.Fatalf("seed team: %v", err)
		}
		return deps
	})
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	form := url.Values{
		"name":     {"Mobile App"},
		"slug":     {"mobile-app"},
		"team":     {"test-org/backend"},
		"platform": {"javascript"},
	}
	resp := sessionRequest(t, client, http.MethodPost, srv.URL+"/manage/projects/", sessionToken, csrf, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if resp.StatusCode != http.StatusSeeOther {
		body := getBody(t, resp)
		t.Fatalf("create project status = %d, want 303; body: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Location"); got != "/settings/project/mobile-app/keys/" {
		t.Fatalf("create project redirect = %q, want keys page", got)
	}
	var selectedCookie bool
	for _, cookie := range resp.Cookies() {
		if cookie.Name == selectedProjectCookie && cookie.Value == "test-org/mobile-app" {
			selectedCookie = true
		}
	}
	resp.Body.Close()
	if !selectedCookie {
		t.Fatal("create project response did not set selected project cookie")
	}

	var projectID string
	if err := db.QueryRow(`SELECT id FROM projects WHERE organization_id = 'test-org' AND slug = 'mobile-app'`).Scan(&projectID); err != nil {
		t.Fatalf("created project lookup: %v", err)
	}
	var keyCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM project_keys WHERE project_id = ? AND status = 'active'`, projectID).Scan(&keyCount); err != nil {
		t.Fatalf("created key count: %v", err)
	}
	if keyCount != 1 {
		t.Fatalf("created key count = %d, want 1", keyCount)
	}

	resp = sessionRequest(t, client, http.MethodGet, srv.URL+"/api/ui/projects", sessionToken, csrf, "", nil)
	body := getBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("project switcher status = %d, want 200; body: %s", resp.StatusCode, body)
	}
	for _, want := range []string{`"value":"test-org/mobile-app"`, `"settingsUrl":"/settings/project/mobile-app/general/"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("project switcher missing %q in %s", want, body)
		}
	}
}

func TestManageProjectsUpdatesAndDeletesProject(t *testing.T) {
	srv, db, sessionToken, csrf := setupAuthorizedTestServerWithDeps(t, func(db *sql.DB, _ *auth.Authorizer, _ string, deps Dependencies) Dependencies {
		if _, err := db.Exec(`INSERT INTO teams (id, organization_id, slug, name) VALUES ('team-1', 'test-org', 'backend', 'Backend')`); err != nil {
			t.Fatalf("seed team: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO projects (id, organization_id, team_id, slug, name, platform) VALUES ('project-edit', 'test-org', 'team-1', 'old-project', 'Old Project', 'go')`); err != nil {
			t.Fatalf("seed project: %v", err)
		}
		return deps
	})
	defer srv.Close()

	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	updateForm := url.Values{"name": {"New Project"}, "slug": {"new-project"}, "platform": {"lua"}}
	resp := sessionRequest(t, client, http.MethodPost, srv.URL+"/manage/projects/test-org/old-project/update", sessionToken, csrf, "application/x-www-form-urlencoded", strings.NewReader(updateForm.Encode()))
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("update project status = %d; body: %s", resp.StatusCode, getBody(t, resp))
	}
	resp.Body.Close()
	var name, platform string
	if err := db.QueryRow(`SELECT name, platform FROM projects WHERE id = 'project-edit' AND slug = 'new-project'`).Scan(&name, &platform); err != nil || name != "New Project" || platform != "lua" {
		t.Fatalf("updated project name=%q platform=%q err=%v", name, platform, err)
	}

	resp = sessionRequest(t, client, http.MethodPost, srv.URL+"/manage/projects/test-org/new-project/delete", sessionToken, csrf, "application/x-www-form-urlencoded", strings.NewReader(""))
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete project status = %d; body: %s", resp.StatusCode, getBody(t, resp))
	}
	resp.Body.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM projects WHERE id = 'project-edit'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted project count=%d err=%v", count, err)
	}
}

func TestManageSidebarLinkPresentInNav(t *testing.T) {
	srv, _, sessionToken, csrf := setupAuthorizedTestServerWithDeps(t, func(_ *sql.DB, _ *auth.Authorizer, _ string, deps Dependencies) Dependencies {
		return deps
	})
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp := sessionRequest(t, client, http.MethodGet, srv.URL+"/manage/", sessionToken, csrf, "", nil)
	body := getBody(t, resp)
	if !strings.Contains(body, "/manage/") {
		t.Errorf("expected /manage/ link in nav sidebar")
	}
	if !strings.Contains(body, `aria-label="Admin"`) {
		t.Errorf("expected Admin nav item in sidebar")
	}
}
