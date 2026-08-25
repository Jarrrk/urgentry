package web

import (
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"urgentry/internal/auth"
	"urgentry/internal/requestmeta"
	sharedstore "urgentry/internal/store"
)

// ---------------------------------------------------------------------------
// Shared manage guard helper
// ---------------------------------------------------------------------------

// manageGuard checks that the request is from an org admin. Returns the first
// organization the caller is authorized to administer, or writes an error
// response and returns nil.
func (h *Handler) manageGuard(w http.ResponseWriter, r *http.Request) *sharedstore.Organization {
	if h.catalog == nil {
		http.Error(w, "Admin console unavailable", http.StatusServiceUnavailable)
		return nil
	}
	orgs, err := h.catalog.ListOrganizations(r.Context())
	if err != nil || len(orgs) == 0 {
		http.Error(w, "Admin console unavailable", http.StatusServiceUnavailable)
		return nil
	}
	for i := range orgs {
		if h.canAdminOrganization(r, orgs[i].Slug) {
			return &orgs[i]
		}
	}
	http.Error(w, "Forbidden", http.StatusForbidden)
	return nil
}

// ---------------------------------------------------------------------------
// Shared page data base
// ---------------------------------------------------------------------------

type manageBase struct {
	Title        string
	Nav          string
	ManageNav    string // active sub-nav key
	Environment  string
	Environments []string
}

// ---------------------------------------------------------------------------
// GET /manage/ — admin dashboard overview
// ---------------------------------------------------------------------------

type manageDashboardData struct {
	manageBase
	OrgCount     int
	ProjectCount int
	UserCount    int
	DBSizeBytes  int64
	DBSizeFmt    string
	Uptime       string
}

func (h *Handler) manageDashboardPage(w http.ResponseWriter, r *http.Request) {
	if h.manageGuard(w, r) == nil {
		return
	}

	orgs, _ := h.catalog.ListOrganizations(r.Context())
	managedOrgs := make([]sharedstore.Organization, 0, len(orgs))
	projectCount := 0
	managedUsers := map[string]struct{}{}
	for _, org := range orgs {
		if !h.canAdminOrganization(r, org.Slug) {
			continue
		}
		managedOrgs = append(managedOrgs, org)
		projects, _ := h.catalog.ListProjects(r.Context(), org.Slug)
		projectCount += len(projects)
		if h.admin != nil {
			members, _ := h.admin.ListOrgMembers(r.Context(), org.Slug)
			for _, member := range members {
				managedUsers[member.UserID] = struct{}{}
			}
		}
	}
	dbSize := h.databaseFileSize()

	h.render(w, "manage-dashboard.html", manageDashboardData{
		manageBase: manageBase{
			Title:        "Admin Console",
			Nav:          "manage",
			ManageNav:    "dashboard",
			Environment:  readSelectedEnvironment(r),
			Environments: h.loadEnvironments(r.Context()),
		},
		OrgCount:     len(managedOrgs),
		ProjectCount: projectCount,
		UserCount:    len(managedUsers),
		DBSizeBytes:  dbSize,
		DBSizeFmt:    formatBytes(dbSize),
		Uptime:       time.Since(h.startedAt).Truncate(time.Second).String(),
	})
}

// ---------------------------------------------------------------------------
// GET /manage/organizations/ — list all organizations
// ---------------------------------------------------------------------------

type manageOrg struct {
	ID           string
	Slug         string
	Name         string
	DateCreated  string
	ProjectCount int
}

type manageOrgsData struct {
	manageBase
	Organizations []manageOrg
	CreateForm    manageOrganizationForm
}

type manageOrganizationForm struct {
	Name  string
	Slug  string
	Error string
}

func (h *Handler) manageOrganizationsPage(w http.ResponseWriter, r *http.Request) {
	if h.manageGuard(w, r) == nil {
		return
	}
	h.renderManageOrganizationsPage(w, r, manageOrganizationForm{})
}

func (h *Handler) renderManageOrganizationsPage(w http.ResponseWriter, r *http.Request, form manageOrganizationForm) {

	orgs, err := h.catalog.ListOrganizations(r.Context())
	if err != nil {
		http.Error(w, "Failed to load organizations", http.StatusInternalServerError)
		return
	}

	items := make([]manageOrg, 0, len(orgs))
	for _, org := range orgs {
		if !h.canAdminOrganization(r, org.Slug) {
			continue
		}
		projects, _ := h.catalog.ListProjects(r.Context(), org.Slug)
		items = append(items, manageOrg{
			ID:           org.ID,
			Slug:         org.Slug,
			Name:         org.Name,
			DateCreated:  timeAgo(org.DateCreated),
			ProjectCount: len(projects),
		})
	}

	h.render(w, "manage-organizations.html", manageOrgsData{
		manageBase: manageBase{
			Title:        "Organizations — Admin Console",
			Nav:          "manage",
			ManageNav:    "organizations",
			Environment:  readSelectedEnvironment(r),
			Environments: h.loadEnvironments(r.Context()),
		},
		Organizations: items,
		CreateForm:    form,
	})
}

func (h *Handler) createManagedOrganization(w http.ResponseWriter, r *http.Request) {
	if h.manageGuard(w, r) == nil {
		return
	}
	if !h.requireManageCSRF(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeWebBadRequest(w, r, "Invalid form")
		return
	}

	form := manageOrganizationForm{
		Name: strings.TrimSpace(r.FormValue("name")),
		Slug: normalizeProjectSlug(r.FormValue("slug")),
	}
	if form.Name == "" {
		form.Error = "Organization name is required."
		h.renderManageOrganizationsPage(w, r, form)
		return
	}
	if form.Slug == "" {
		form.Slug = normalizeProjectSlug(form.Name)
	}
	if form.Slug == "" {
		form.Error = "Organization slug must contain a letter or number."
		h.renderManageOrganizationsPage(w, r, form)
		return
	}
	existing, err := h.catalog.GetOrganization(r.Context(), form.Slug)
	if err != nil {
		writeWebInternal(w, r, "Failed to check organization slug.")
		return
	}
	if existing != nil {
		form.Error = "An organization with that slug already exists."
		h.renderManageOrganizationsPage(w, r, form)
		return
	}
	principal := auth.PrincipalFromContext(r.Context())
	if principal == nil || principal.User == nil || principal.User.ID == "" {
		writeWebForbidden(w, r)
		return
	}
	org, err := h.catalog.CreateOrganization(r.Context(), sharedstore.OrganizationCreateInput{Name: form.Name, Slug: form.Slug}, principal.User.ID)
	if err != nil || org == nil {
		form.Error = "Failed to create organization."
		h.renderManageOrganizationsPage(w, r, form)
		return
	}
	http.Redirect(w, r, "/manage/organizations/", http.StatusSeeOther)
}

func (h *Handler) updateManagedOrganization(w http.ResponseWriter, r *http.Request) {
	if h.manageGuard(w, r) == nil {
		return
	}
	if !h.requireManageCSRF(w, r) {
		return
	}
	orgSlug := strings.TrimSpace(r.PathValue("org_slug"))
	if !h.canAdminOrganization(r, orgSlug) {
		writeWebForbidden(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeWebBadRequest(w, r, "Invalid form")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	newSlug := normalizeProjectSlug(r.FormValue("slug"))
	if name == "" || newSlug == "" {
		writeWebBadRequest(w, r, "Name and slug are required")
		return
	}
	updated, err := h.catalog.UpdateOrganization(r.Context(), orgSlug, sharedstore.OrganizationUpdate{Name: name, Slug: newSlug})
	if err != nil || updated == nil {
		writeWebInternal(w, r, "Failed to update organization.")
		return
	}
	http.Redirect(w, r, "/manage/organizations/", http.StatusSeeOther)
}

func (h *Handler) canAdminOrganization(r *http.Request, orgSlug string) bool {
	return h.authz == nil || h.authz.AuthorizeOrganization(r, orgSlug, auth.ScopeOrgAdmin) == nil
}

func (h *Handler) requireManageCSRF(w http.ResponseWriter, r *http.Request) bool {
	if h.authz != nil && !h.authz.ValidateCSRF(r) {
		writeWebForbidden(w, r)
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// GET /manage/projects/ — list all projects across orgs
// ---------------------------------------------------------------------------

type manageProject struct {
	ID          string
	Slug        string
	Name        string
	OrgSlug     string
	Platform    string
	Status      string
	DateCreated string
	TeamSlug    string
}

type manageProjectTeamOption struct {
	Value    string
	OrgSlug  string
	TeamSlug string
	Label    string
}

type manageCreateProjectForm struct {
	Name      string
	Slug      string
	TeamValue string
	Platform  string
	Error     string
	Teams     []manageProjectTeamOption
}

type manageProjectsData struct {
	manageBase
	Projects   []manageProject
	CreateForm manageCreateProjectForm
}

func (h *Handler) manageProjectsPage(w http.ResponseWriter, r *http.Request) {
	if h.manageGuard(w, r) == nil {
		return
	}
	h.renderManageProjectsPage(w, r, manageCreateProjectForm{Platform: "go"})
}

func (h *Handler) renderManageProjectsPage(w http.ResponseWriter, r *http.Request, form manageCreateProjectForm) {
	// ListProjects with empty org returns all projects.
	projects, err := h.catalog.ListProjects(r.Context(), "")
	if err != nil {
		http.Error(w, "Failed to load projects", http.StatusInternalServerError)
		return
	}

	items := make([]manageProject, 0, len(projects))
	for _, p := range projects {
		if !h.canAdminOrganization(r, p.OrgSlug) {
			continue
		}
		items = append(items, manageProject{
			ID:          p.ID,
			Slug:        p.Slug,
			Name:        p.Name,
			OrgSlug:     p.OrgSlug,
			Platform:    p.Platform,
			Status:      p.Status,
			DateCreated: timeAgo(p.DateCreated),
			TeamSlug:    p.TeamSlug,
		})
	}
	teams, err := h.manageProjectTeamOptions(r)
	if err != nil {
		http.Error(w, "Failed to load teams", http.StatusInternalServerError)
		return
	}
	form.Teams = teams

	h.render(w, "manage-projects.html", manageProjectsData{
		manageBase: manageBase{
			Title:        "Projects — Admin Console",
			Nav:          "manage",
			ManageNav:    "projects",
			Environment:  readSelectedEnvironment(r),
			Environments: h.loadEnvironments(r.Context()),
		},
		Projects:   items,
		CreateForm: form,
	})
}

func (h *Handler) createManagedProject(w http.ResponseWriter, r *http.Request) {
	if h.manageGuard(w, r) == nil {
		return
	}
	if !h.requireManageCSRF(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeWebBadRequest(w, r, "Invalid form")
		return
	}

	form := manageCreateProjectForm{
		Name:      strings.TrimSpace(r.FormValue("name")),
		Slug:      strings.TrimSpace(r.FormValue("slug")),
		TeamValue: strings.TrimSpace(r.FormValue("team")),
		Platform:  strings.TrimSpace(r.FormValue("platform")),
	}
	if form.Platform == "" {
		form.Platform = "go"
	}
	if form.Name == "" {
		h.renderManageProjectsPage(w, r, withCreateProjectError(form, "Project name is required."))
		return
	}
	orgSlug, teamSlug, ok := splitProjectSwitcherValue(form.TeamValue)
	if !ok {
		h.renderManageProjectsPage(w, r, withCreateProjectError(form, "Team is required."))
		return
	}
	slug := normalizeProjectSlug(form.Slug)
	if slug == "" {
		slug = normalizeProjectSlug(form.Name)
	}
	if slug == "" {
		h.renderManageProjectsPage(w, r, withCreateProjectError(form, "Project slug must contain a letter or number."))
		return
	}
	form.Slug = slug
	if !h.canAdminOrganization(r, orgSlug) {
		writeWebForbidden(w, r)
		return
	}

	existing, err := h.catalog.GetProject(r.Context(), orgSlug, slug)
	if err != nil {
		http.Error(w, "Failed to check project slug", http.StatusInternalServerError)
		return
	}
	if existing != nil {
		h.renderManageProjectsPage(w, r, withCreateProjectError(form, "A project with that slug already exists in this organization."))
		return
	}

	project, err := h.catalog.CreateProject(r.Context(), orgSlug, teamSlug, sharedstore.ProjectCreateInput{
		Name:     form.Name,
		Slug:     slug,
		Platform: form.Platform,
	})
	if err != nil {
		h.renderManageProjectsPage(w, r, withCreateProjectError(form, "Failed to create project."))
		return
	}
	if project == nil {
		h.renderManageProjectsPage(w, r, withCreateProjectError(form, "Organization or team not found."))
		return
	}
	if _, err := h.catalog.CreateProjectKey(r.Context(), project.OrgSlug, project.Slug, "Default"); err != nil {
		http.Error(w, "Failed to create project key", http.StatusInternalServerError)
		return
	}

	setSelectedProjectCookie(w, project.OrgSlug, project.Slug)
	http.Redirect(w, r, "/settings/project/"+url.PathEscape(project.Slug)+"/keys/", http.StatusSeeOther)
}

func withCreateProjectError(form manageCreateProjectForm, message string) manageCreateProjectForm {
	form.Error = message
	return form
}

func (h *Handler) updateManagedProject(w http.ResponseWriter, r *http.Request) {
	if h.manageGuard(w, r) == nil {
		return
	}
	if !h.requireManageCSRF(w, r) {
		return
	}
	orgSlug := strings.TrimSpace(r.PathValue("org_slug"))
	projectSlug := strings.TrimSpace(r.PathValue("project_slug"))
	if !h.canAdminOrganization(r, orgSlug) {
		writeWebForbidden(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeWebBadRequest(w, r, "Invalid form")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	newSlug := normalizeProjectSlug(r.FormValue("slug"))
	platform := strings.TrimSpace(r.FormValue("platform"))
	if name == "" || newSlug == "" {
		writeWebBadRequest(w, r, "Name and slug are required")
		return
	}
	updated, err := h.catalog.UpdateProject(r.Context(), orgSlug, projectSlug, sharedstore.ProjectUpdate{
		Name:     &name,
		Slug:     &newSlug,
		Platform: &platform,
	})
	if err != nil || updated == nil {
		writeWebInternal(w, r, "Failed to update project.")
		return
	}
	http.Redirect(w, r, "/manage/projects/", http.StatusSeeOther)
}

func (h *Handler) deleteManagedProject(w http.ResponseWriter, r *http.Request) {
	if h.manageGuard(w, r) == nil {
		return
	}
	if !h.requireManageCSRF(w, r) {
		return
	}
	orgSlug := strings.TrimSpace(r.PathValue("org_slug"))
	projectSlug := strings.TrimSpace(r.PathValue("project_slug"))
	if !h.canAdminOrganization(r, orgSlug) {
		writeWebForbidden(w, r)
		return
	}
	if err := h.catalog.DeleteProject(r.Context(), orgSlug, projectSlug); err != nil {
		writeWebInternal(w, r, "Failed to delete project.")
		return
	}
	http.Redirect(w, r, "/manage/projects/", http.StatusSeeOther)
}

func (h *Handler) manageProjectTeamOptions(r *http.Request) ([]manageProjectTeamOption, error) {
	orgs, err := h.catalog.ListOrganizations(r.Context())
	if err != nil {
		return nil, err
	}
	options := []manageProjectTeamOption{}
	for _, org := range orgs {
		if !h.canAdminOrganization(r, org.Slug) {
			continue
		}
		teams, err := h.catalog.ListTeams(r.Context(), org.Slug)
		if err != nil {
			return nil, err
		}
		for _, team := range teams {
			options = append(options, manageProjectTeamOption{
				Value:    projectSwitcherValue(org.Slug, team.Slug),
				OrgSlug:  org.Slug,
				TeamSlug: team.Slug,
				Label:    org.Slug + " / " + team.Slug,
			})
		}
	}
	return options, nil
}

// ---------------------------------------------------------------------------
// GET /manage/users/ — list all users with role badges
// ---------------------------------------------------------------------------

type manageUser struct {
	ID        string
	Email     string
	Name      string
	OrgRoles  []manageUserOrgRole
	CreatedAt string
}

type manageUserOrgRole struct {
	MemberID string
	OrgSlug  string
	Role     string
}

type manageUsersData struct {
	manageBase
	Users      []manageUser
	Invites    []manageInvite
	InviteForm manageInviteForm
}

type manageInvite struct {
	ID        string
	OrgSlug   string
	TeamSlug  string
	Email     string
	Role      string
	Status    string
	ExpiresAt string
}

type manageInviteOrgOption struct {
	Slug  string
	Teams []manageInviteTeamOption
}

type manageInviteTeamOption struct {
	Value string
	Label string
}

type manageInviteForm struct {
	Email     string
	OrgSlug   string
	TeamValue string
	Role      string
	Error     string
	InviteURL string
	Orgs      []manageInviteOrgOption
}

func (h *Handler) manageUsersPage(w http.ResponseWriter, r *http.Request) {
	if h.manageGuard(w, r) == nil {
		return
	}
	h.renderManageUsersPage(w, r, manageInviteForm{Role: "member"})
}

func (h *Handler) renderManageUsersPage(w http.ResponseWriter, r *http.Request, form manageInviteForm) {
	if h.admin == nil {
		http.Error(w, "User management unavailable", http.StatusServiceUnavailable)
		return
	}

	orgs, err := h.catalog.ListOrganizations(r.Context())
	if err != nil {
		http.Error(w, "Failed to load users", http.StatusInternalServerError)
		return
	}
	usersByID := map[string]*manageUser{}
	invites := []manageInvite{}
	for _, org := range orgs {
		if !h.canAdminOrganization(r, org.Slug) {
			continue
		}
		teams, err := h.admin.ListTeams(r.Context(), org.Slug)
		if err != nil {
			http.Error(w, "Failed to load teams", http.StatusInternalServerError)
			return
		}
		orgOption := manageInviteOrgOption{Slug: org.Slug}
		for _, team := range teams {
			orgOption.Teams = append(orgOption.Teams, manageInviteTeamOption{Value: projectSwitcherValue(org.Slug, team.Slug), Label: org.Slug + " / " + team.Slug})
		}
		form.Orgs = append(form.Orgs, orgOption)
		members, err := h.admin.ListOrgMembers(r.Context(), org.Slug)
		if err != nil {
			http.Error(w, "Failed to load users", http.StatusInternalServerError)
			return
		}
		for _, member := range members {
			user := usersByID[member.UserID]
			if user == nil {
				user = &manageUser{ID: member.UserID, Email: member.Email, Name: member.Name, CreatedAt: timeAgo(member.CreatedAt)}
				usersByID[member.UserID] = user
			}
			user.OrgRoles = append(user.OrgRoles, manageUserOrgRole{MemberID: member.ID, OrgSlug: org.Slug, Role: member.Role})
		}
		orgInvites, err := h.admin.ListInvites(r.Context(), org.Slug)
		if err != nil {
			http.Error(w, "Failed to load invitations", http.StatusInternalServerError)
			return
		}
		for _, invite := range orgInvites {
			expires := "-"
			if invite.ExpiresAt != nil {
				expires = timeAgo(*invite.ExpiresAt)
			}
			invites = append(invites, manageInvite{ID: invite.ID, OrgSlug: org.Slug, TeamSlug: invite.TeamSlug, Email: invite.Email, Role: invite.Role, Status: invite.Status, ExpiresAt: expires})
		}
	}
	users := make([]manageUser, 0, len(usersByID))
	for _, user := range usersByID {
		users = append(users, *user)
	}

	h.render(w, "manage-users.html", manageUsersData{
		manageBase: manageBase{
			Title:        "Users — Admin Console",
			Nav:          "manage",
			ManageNav:    "users",
			Environment:  readSelectedEnvironment(r),
			Environments: h.loadEnvironments(r.Context()),
		},
		Users:      users,
		Invites:    invites,
		InviteForm: form,
	})
}

func (h *Handler) createManagedInvite(w http.ResponseWriter, r *http.Request) {
	if h.manageGuard(w, r) == nil {
		return
	}
	if !h.requireManageCSRF(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeWebBadRequest(w, r, "Invalid form")
		return
	}
	form := manageInviteForm{
		Email:     strings.TrimSpace(r.FormValue("email")),
		OrgSlug:   strings.TrimSpace(r.FormValue("organization")),
		TeamValue: strings.TrimSpace(r.FormValue("team")),
		Role:      strings.TrimSpace(r.FormValue("role")),
	}
	if form.Email == "" || form.OrgSlug == "" {
		form.Error = "Email and organization are required."
		h.renderManageUsersPage(w, r, form)
		return
	}
	if !validManagedOrgRole(form.Role) {
		form.Error = "Invalid organization role."
		h.renderManageUsersPage(w, r, form)
		return
	}
	if !h.canAdminOrganization(r, form.OrgSlug) {
		writeWebForbidden(w, r)
		return
	}
	teamSlug := ""
	if form.TeamValue != "" {
		teamOrgSlug, parsedTeamSlug, ok := splitProjectSwitcherValue(form.TeamValue)
		if !ok || teamOrgSlug != form.OrgSlug {
			form.Error = "Selected team does not belong to the organization."
			h.renderManageUsersPage(w, r, form)
			return
		}
		teamSlug = parsedTeamSlug
	}
	principal := auth.PrincipalFromContext(r.Context())
	if principal == nil || principal.User == nil {
		writeWebForbidden(w, r)
		return
	}
	invite, token, err := h.admin.CreateInvite(r.Context(), form.OrgSlug, form.Email, form.Role, teamSlug, principal.User.ID)
	if err != nil || invite == nil || token == "" {
		form.Error = "Failed to create invitation."
		h.renderManageUsersPage(w, r, form)
		return
	}
	form.InviteURL = requestmeta.Scheme(r) + "://" + requestmeta.Host(r) + "/accept-invite/" + url.PathEscape(token) + "/"
	h.renderManageUsersPage(w, r, form)
}

func (h *Handler) updateManagedUserRole(w http.ResponseWriter, r *http.Request) {
	if h.manageGuard(w, r) == nil {
		return
	}
	if !h.requireManageCSRF(w, r) {
		return
	}
	orgSlug := strings.TrimSpace(r.PathValue("org_slug"))
	memberID := strings.TrimSpace(r.PathValue("member_id"))
	if !h.canAdminOrganization(r, orgSlug) {
		writeWebForbidden(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeWebBadRequest(w, r, "Invalid form")
		return
	}
	role := strings.TrimSpace(r.FormValue("role"))
	if !validManagedOrgRole(role) {
		writeWebBadRequest(w, r, "Invalid organization role")
		return
	}
	updated, err := h.admin.UpdateOrgMemberRole(r.Context(), orgSlug, memberID, role)
	if err != nil || updated == nil {
		writeWebBadRequest(w, r, "Unable to update member role")
		return
	}
	http.Redirect(w, r, "/manage/users/", http.StatusSeeOther)
}

func (h *Handler) removeManagedUser(w http.ResponseWriter, r *http.Request) {
	if h.manageGuard(w, r) == nil {
		return
	}
	if !h.requireManageCSRF(w, r) {
		return
	}
	orgSlug := strings.TrimSpace(r.PathValue("org_slug"))
	memberID := strings.TrimSpace(r.PathValue("member_id"))
	if !h.canAdminOrganization(r, orgSlug) {
		writeWebForbidden(w, r)
		return
	}
	removed, err := h.admin.RemoveOrgMember(r.Context(), orgSlug, memberID)
	if err != nil || !removed {
		writeWebBadRequest(w, r, "Unable to remove member")
		return
	}
	http.Redirect(w, r, "/manage/users/", http.StatusSeeOther)
}

func (h *Handler) revokeManagedInvite(w http.ResponseWriter, r *http.Request) {
	if h.manageGuard(w, r) == nil {
		return
	}
	if !h.requireManageCSRF(w, r) {
		return
	}
	orgSlug := strings.TrimSpace(r.PathValue("org_slug"))
	if !h.canAdminOrganization(r, orgSlug) {
		writeWebForbidden(w, r)
		return
	}
	revoked, err := h.admin.RevokeInvite(r.Context(), orgSlug, strings.TrimSpace(r.PathValue("invite_id")))
	if err != nil || !revoked {
		writeWebBadRequest(w, r, "Unable to revoke invitation")
		return
	}
	http.Redirect(w, r, "/manage/users/", http.StatusSeeOther)
}

func validManagedOrgRole(role string) bool {
	switch role {
	case "owner", "admin", "manager", "member":
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// GET /manage/settings/ — system-level settings (retention, quotas)
// ---------------------------------------------------------------------------

type manageSettingsProject struct {
	Slug                    string
	Name                    string
	OrgSlug                 string
	EventRetentionDays      int
	AttachmentRetentionDays int
	DebugRetentionDays      int
}

type manageSettingsData struct {
	manageBase
	Projects []manageSettingsProject
}

func (h *Handler) manageSettingsPage(w http.ResponseWriter, r *http.Request) {
	if h.manageGuard(w, r) == nil {
		return
	}

	projects, err := h.catalog.ListProjects(r.Context(), "")
	if err != nil {
		http.Error(w, "Failed to load settings", http.StatusInternalServerError)
		return
	}

	items := make([]manageSettingsProject, 0, len(projects))
	for _, p := range projects {
		if !h.canAdminOrganization(r, p.OrgSlug) {
			continue
		}
		settings, sErr := h.catalog.GetProjectSettings(r.Context(), p.OrgSlug, p.Slug)
		if sErr != nil || settings == nil {
			items = append(items, manageSettingsProject{
				Slug:    p.Slug,
				Name:    p.Name,
				OrgSlug: p.OrgSlug,
			})
			continue
		}
		items = append(items, manageSettingsProject{
			Slug:                    p.Slug,
			Name:                    p.Name,
			OrgSlug:                 p.OrgSlug,
			EventRetentionDays:      settings.EventRetentionDays,
			AttachmentRetentionDays: settings.AttachmentRetentionDays,
			DebugRetentionDays:      settings.DebugFileRetentionDays,
		})
	}

	h.render(w, "manage-settings.html", manageSettingsData{
		manageBase: manageBase{
			Title:        "Settings — Admin Console",
			Nav:          "manage",
			ManageNav:    "settings",
			Environment:  readSelectedEnvironment(r),
			Environments: h.loadEnvironments(r.Context()),
		},
		Projects: items,
	})
}

// ---------------------------------------------------------------------------
// GET /manage/status/ — environment info, Go version, database stats
// ---------------------------------------------------------------------------

type manageStatusData struct {
	manageBase
	GoVersion    string
	GOOS         string
	GOARCH       string
	NumCPU       int
	NumGoroutine int
	DBSizeBytes  int64
	DBSizeFmt    string
	DBOpenConns  int
	DBIdleConns  int
	Uptime       string
	StartedAt    string
}

func (h *Handler) manageStatusPage(w http.ResponseWriter, r *http.Request) {
	if h.manageGuard(w, r) == nil {
		return
	}

	dbStats := h.db.Stats()
	dbSize := h.databaseFileSize()

	h.render(w, "manage-status.html", manageStatusData{
		manageBase: manageBase{
			Title:        "Status — Admin Console",
			Nav:          "manage",
			ManageNav:    "status",
			Environment:  readSelectedEnvironment(r),
			Environments: h.loadEnvironments(r.Context()),
		},
		GoVersion:    runtime.Version(),
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
		DBSizeBytes:  dbSize,
		DBSizeFmt:    formatBytes(dbSize),
		DBOpenConns:  dbStats.OpenConnections,
		DBIdleConns:  dbStats.Idle,
		Uptime:       time.Since(h.startedAt).Truncate(time.Second).String(),
		StartedAt:    h.startedAt.UTC().Format(time.RFC3339),
	})
}
