package issueupdates

import "testing"

func TestBrokerPublishesIssueAndProjectVersions(t *testing.T) {
	b := NewBroker()
	b.Publish("project-1", "issue-1")
	b.Publish("project-1", "issue-1")

	if got := b.IssueVersion("issue-1"); got != 2 {
		t.Fatalf("issue version = %d, want 2", got)
	}
	if got := b.ProjectVersion("project-1"); got != 2 {
		t.Fatalf("project version = %d, want 2", got)
	}
	if got := b.IssueVersion("issue-2"); got != 0 {
		t.Fatalf("unrelated issue version = %d, want 0", got)
	}
}
