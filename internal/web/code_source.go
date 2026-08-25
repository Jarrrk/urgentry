package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"urgentry/internal/store"
)

const (
	codeSourceContextRadius = 5
	codeSourceMaxBytes      = 2 << 20
	codeSourceMaxFiles      = 5
)

type forgejoSourceClient struct {
	baseURL *url.URL
	token   string
	client  *http.Client
}

func newForgejoSourceClient(rawBaseURL, token string, client *http.Client) *forgejoSourceClient {
	baseURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(rawBaseURL), "/"))
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return nil
	}
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &forgejoSourceClient{baseURL: baseURL, token: strings.TrimSpace(token), client: &clientCopy}
}

func (c *forgejoSourceClient) fetch(ctx context.Context, mapping *store.CodeMapping, repoPath string) ([]string, error) {
	provider, _ := store.NormalizeCodeMappingProvider(mapping.Provider)
	if provider != store.CodeMappingProviderForgejo && provider != store.CodeMappingProviderGitea {
		return nil, fmt.Errorf("unsupported source provider %q", provider)
	}
	repositoryURL, err := url.Parse(mapping.RepoURL)
	if err != nil || !strings.EqualFold(repositoryURL.Scheme, c.baseURL.Scheme) || !strings.EqualFold(repositoryURL.Host, c.baseURL.Host) {
		return nil, fmt.Errorf("repository URL is outside the configured Forgejo origin")
	}

	basePath := strings.Trim(c.baseURL.Path, "/")
	repositoryPath := strings.Trim(repositoryURL.Path, "/")
	if basePath != "" {
		if repositoryPath == basePath || !strings.HasPrefix(repositoryPath, basePath+"/") {
			return nil, fmt.Errorf("repository URL is outside the configured Forgejo base path")
		}
		repositoryPath = strings.TrimPrefix(repositoryPath, basePath+"/")
	}
	parts := strings.Split(repositoryPath, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("repository URL must end with owner/repository")
	}
	owner := parts[0]
	repository := strings.TrimSuffix(parts[1], ".git")

	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(c.baseURL.Path, "/") + "/api/v1/repos/" + owner + "/" + repository + "/raw/" + repoPath
	query := endpoint.Query()
	branch := strings.TrimSpace(mapping.DefaultBranch)
	if branch == "" {
		branch = "main"
	}
	query.Set("ref", branch)
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "text/plain")
	if c.token != "" {
		request.Header.Set("Authorization", "token "+c.token)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Forgejo returned %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, codeSourceMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > codeSourceMaxBytes {
		return nil, fmt.Errorf("source file exceeds %d bytes", codeSourceMaxBytes)
	}
	return strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n"), nil
}

func (h *Handler) applyCodeSourceContext(ctx context.Context, groups []exceptionGroup, frames []stackFrame, mappings []*store.CodeMapping) {
	if h.codeSource == nil || len(mappings) == 0 {
		collapseSourceContexts(groups, frames)
		return
	}
	cache := make(map[string][]string)
	load := func(filename string) []string {
		mapping, repoPath := matchingCodeMapping(filename, mappings)
		if mapping == nil {
			return nil
		}
		key := mapping.RepoURL + "\x00" + mapping.DefaultBranch + "\x00" + repoPath
		if lines, ok := cache[key]; ok {
			return lines
		}
		if len(cache) >= codeSourceMaxFiles {
			return nil
		}
		lines, err := h.codeSource.fetch(ctx, mapping, repoPath)
		if err != nil {
			cache[key] = nil
			return nil
		}
		cache[key] = lines
		return lines
	}

	for gi := range groups {
		for fi := range groups[gi].Frames {
			frame := &groups[gi].Frames[fi]
			if frame.HasContext || frame.LineNo <= 0 {
				continue
			}
			applyRichSourceContext(frame, load(frame.File))
		}
	}
	for i := range frames {
		if len(frames[i].CodeLines) > 0 || frames[i].LineNo <= 0 {
			continue
		}
		frames[i].CodeLines = sourceCodeLines(load(frames[i].File), frames[i].LineNo)
	}
	collapseSourceContexts(groups, frames)
}

func collapseSourceContexts(groups []exceptionGroup, frames []stackFrame) {
	firstRichContext := true
	for gi := range groups {
		for fi := range groups[gi].Frames {
			frame := &groups[gi].Frames[fi]
			frame.Collapsed = !frame.HasContext || !firstRichContext
			if frame.HasContext {
				firstRichContext = false
			}
		}
	}
	firstFlatContext := true
	for i := range frames {
		frames[i].Collapsed = len(frames[i].CodeLines) == 0 || !firstFlatContext
		if len(frames[i].CodeLines) > 0 {
			firstFlatContext = false
		}
	}
}

func applyRichSourceContext(frame *richFrame, lines []string) {
	if frame.LineNo <= 0 || frame.LineNo > len(lines) {
		return
	}
	start, end := sourceContextBounds(len(lines), frame.LineNo)
	for number := start; number < frame.LineNo; number++ {
		frame.PreContext = append(frame.PreContext, contextLine{Number: number, Content: lines[number-1]})
	}
	frame.ContextLine = lines[frame.LineNo-1]
	for number := frame.LineNo + 1; number <= end; number++ {
		frame.PostContext = append(frame.PostContext, contextLine{Number: number, Content: lines[number-1]})
	}
	frame.HasContext = true
}

func sourceCodeLines(lines []string, lineNo int) []codeLine {
	if lineNo <= 0 || lineNo > len(lines) {
		return nil
	}
	start, end := sourceContextBounds(len(lines), lineNo)
	result := make([]codeLine, 0, end-start+1)
	for number := start; number <= end; number++ {
		result = append(result, codeLine{Number: number, Content: lines[number-1], Highlight: number == lineNo})
	}
	return result
}

func sourceContextBounds(lineCount, lineNo int) (int, int) {
	start := lineNo - codeSourceContextRadius
	if start < 1 {
		start = 1
	}
	end := lineNo + codeSourceContextRadius
	if end > lineCount {
		end = lineCount
	}
	return start, end
}
