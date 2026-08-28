// Package issueupdates provides lightweight in-process notifications for issue
// list and detail pages. Notifications contain no issue data; subscribers use
// them only as a signal to reload from the source of truth.
package issueupdates

import "sync"

// Broker fans issue changes out to interested browser connections.
type Broker struct {
	mu       sync.RWMutex
	nextID   uint64
	issues   map[string]map[uint64]chan struct{}
	projects map[string]map[uint64]chan struct{}
}

func NewBroker() *Broker {
	return &Broker{
		issues:   make(map[string]map[uint64]chan struct{}),
		projects: make(map[string]map[uint64]chan struct{}),
	}
}

func (b *Broker) SubscribeIssue(issueID string) (<-chan struct{}, func()) {
	return b.subscribe(b.issues, issueID)
}

func (b *Broker) SubscribeProject(projectID string) (<-chan struct{}, func()) {
	return b.subscribe(b.projects, projectID)
}

func (b *Broker) subscribe(target map[string]map[uint64]chan struct{}, key string) (<-chan struct{}, func()) {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	ch := make(chan struct{}, 1)
	if target[key] == nil {
		target[key] = make(map[uint64]chan struct{})
	}
	target[key][id] = ch
	b.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(target[key], id)
			if len(target[key]) == 0 {
				delete(target, key)
			}
			b.mu.Unlock()
		})
	}
}

// Publish signals both viewers of the issue and viewers of its project list.
// A slow subscriber keeps at most one pending signal.
func (b *Broker) Publish(projectID, issueID string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	b.publish(b.issues[issueID])
	b.publish(b.projects[projectID])
}

func (b *Broker) publish(subscribers map[uint64]chan struct{}) {
	for _, ch := range subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
