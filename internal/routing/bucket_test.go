package routing

import (
	"fmt"
	"testing"

	"github.com/WanderningMaster/peerdrive/configuration"
	id "github.com/WanderningMaster/peerdrive/internal/id"
)

func mkContact(tag string) Contact {
	return Contact{ID: id.HashKey(tag), Addr: tag}
}

func TestBucketTouchInsertMoveAndEvict(t *testing.T) {
	var b Bucket
	defaults := configuration.Default()

	var inserted []Contact
	for i := 0; i < defaults.KBucketK; i++ {
		c := mkContact(fmt.Sprintf("c%02d", i))
		if ev := b.Touch(c); ev != nil {
			t.Fatalf("unexpected eviction while filling: %+v", *ev)
		}
		inserted = append(inserted, c)
	}

	if got := len(b.Contacts()); got != defaults.KBucketK {
		t.Fatalf("unexpected length: got %d want %d", got, defaults.KBucketK)
	}

	pivot := inserted[5]
	if ev := b.Touch(pivot); ev != nil {
		t.Fatalf("touch existing should not evict, got: %+v", *ev)
	}
	after := b.Contacts()
	if after[len(after)-1].ID != pivot.ID {
		t.Fatalf("existing contact was not moved to end")
	}

	lruBefore := after[0]

	newcomer := mkContact("newcomer")
	ev := b.Touch(newcomer)
	if ev == nil {
		t.Fatalf("expected eviction on full bucket")
	}
	if ev.ID != lruBefore.ID {
		t.Fatalf("evicted wrong contact: got %q want %q", ev.ID.String(), lruBefore.ID.String())
	}
	final := b.Contacts()
	if len(final) != defaults.KBucketK {
		t.Fatalf("bucket size changed unexpectedly: got %d want %d", len(final), defaults.KBucketK)
	}
	if final[len(final)-1].ID != newcomer.ID {
		t.Fatalf("new contact not at end")
	}
}

func TestBucketContactsDefensiveCopy(t *testing.T) {
	var b Bucket
	a := mkContact("A")
	b1 := mkContact("B")
	b.Touch(a)
	b.Touch(b1)

	snap := b.Contacts()
	snap[0].Addr = "mutated"

	snap2 := b.Contacts()
	if snap2[0].Addr == "mutated" {
		t.Fatalf("Contacts() did not return a defensive copy")
	}
}
