package store

import (
	"context"
	"strings"
	"time"
)

const (
	CodeMappingProviderGitHub  = "github"
	CodeMappingProviderGitLab  = "gitlab"
	CodeMappingProviderForgejo = "forgejo"
	CodeMappingProviderGitea   = "gitea"
)

// CodeMapping maps stack trace file paths to source code URLs in a repository.
// When a frame's filename starts with StackRoot, the matching prefix is
// replaced with SourceRoot and appended to the DefaultBranch URL pattern.
type CodeMapping struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"projectId"`
	StackRoot     string    `json:"stackRoot"`     // prefix in stack frames, e.g. "src/"
	SourceRoot    string    `json:"sourceRoot"`    // prefix in repo, e.g. "app/src/"
	DefaultBranch string    `json:"defaultBranch"` // e.g. "main"
	RepoURL       string    `json:"repoUrl"`       // e.g. "https://github.com/org/repo"
	Provider      string    `json:"provider"`      // github, gitlab, forgejo, or gitea
	CreatedAt     time.Time `json:"createdAt"`
}

// NormalizeCodeMappingProvider returns a supported provider name. An omitted
// provider retains the historical GitHub/GitLab-style link format.
func NormalizeCodeMappingProvider(provider string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", CodeMappingProviderGitHub:
		return CodeMappingProviderGitHub, true
	case CodeMappingProviderGitLab:
		return CodeMappingProviderGitLab, true
	case CodeMappingProviderForgejo:
		return CodeMappingProviderForgejo, true
	case CodeMappingProviderGitea:
		return CodeMappingProviderGitea, true
	default:
		return "", false
	}
}

// CodeMappingStore persists and retrieves code mapping configurations.
type CodeMappingStore interface {
	CreateCodeMapping(ctx context.Context, m *CodeMapping) error
	ListCodeMappings(ctx context.Context, projectID string) ([]*CodeMapping, error)
	DeleteCodeMapping(ctx context.Context, projectID, id string) error
}
