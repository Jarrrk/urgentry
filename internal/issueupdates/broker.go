// Package issueupdates tracks lightweight in-process change versions for issue
// list and detail pages. Versions contain no issue data; browsers use them only
// as a signal to reload from the source of truth.
package issueupdates

import "sync"

// Broker fans issue changes out to interested browser connections.
type Broker struct {
	mu       sync.RWMutex
	issues   map[string]uint64
	projects map[string]uint64
}

func NewBroker() *Broker {
	return &Broker{
		issues:   make(map[string]uint64),
		projects: make(map[string]uint64),
	}
}

func (b *Broker) IssueVersion(issueID string) uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.issues[issueID]
}

func (b *Broker) ProjectVersion(projectID string) uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.projects[projectID]
}

// Publish advances both the issue and project-list versions.
func (b *Broker) Publish(projectID, issueID string) {
	b.mu.Lock()
	b.issues[issueID]++
	b.projects[projectID]++
	b.mu.Unlock()
}
