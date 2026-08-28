package issueupdates

import (
	"testing"
	"time"
)

func TestBrokerPublishesToIssueAndProjectSubscribers(t *testing.T) {
	b := NewBroker()
	issueCh, unsubscribeIssue := b.SubscribeIssue("issue-1")
	defer unsubscribeIssue()
	projectCh, unsubscribeProject := b.SubscribeProject("project-1")
	defer unsubscribeProject()
	otherCh, unsubscribeOther := b.SubscribeIssue("issue-2")
	defer unsubscribeOther()

	b.Publish("project-1", "issue-1")

	assertSignalled(t, issueCh)
	assertSignalled(t, projectCh)
	select {
	case <-otherCh:
		t.Fatal("unrelated issue subscriber was signalled")
	default:
	}
}

func TestBrokerCoalescesPendingSignalsAndUnsubscribes(t *testing.T) {
	b := NewBroker()
	ch, unsubscribe := b.SubscribeIssue("issue-1")
	b.Publish("project-1", "issue-1")
	b.Publish("project-1", "issue-1")
	assertSignalled(t, ch)
	select {
	case <-ch:
		t.Fatal("expected duplicate pending signals to be coalesced")
	default:
	}

	unsubscribe()
	b.Publish("project-1", "issue-1")
	select {
	case <-ch:
		t.Fatal("unsubscribed channel was signalled")
	default:
	}
}

func assertSignalled(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("subscriber was not signalled")
	}
}
