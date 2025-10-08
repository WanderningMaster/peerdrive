package routing

import (
	"testing"

	"github.com/WanderningMaster/peerdrive/configuration"
	id "github.com/WanderningMaster/peerdrive/internal/id"
)

func idWithFirstOneAt(bit int) id.NodeID {
	var out id.NodeID
	byteIdx := bit / 8
	bitInByte := bit % 8
	out[byteIdx] = 1 << (7 - bitInByte)
	return out
}

func TestBucketIndexPositions(t *testing.T) {
	var self id.NodeID // all zeros
	rt := NewRoutingTable(self)

	for _, b := range []int{0, 1, 7, 8, 9, 255} {
		nid := idWithFirstOneAt(b)
		if got := rt.BucketIndex(nid); got != b {
			t.Fatalf("BucketIndex bit %d: got %d want %d", b, got, b)
		}
	}

	defaults := configuration.Default()
	if got, want := rt.BucketIndex(self), defaults.IdBits-1; got != want {
		t.Fatalf("BucketIndex(self): got %d want %d", got, want)
	}
}

func TestUpdateSkipsSelfAndPlacesCorrectBucket(t *testing.T) {
	var self id.NodeID
	rt := NewRoutingTable(self)

	rt.Update(Contact{ID: self, Addr: "self"})
	for i, b := range rt.buckets {
		if n := len(b.Contacts()); n != 0 {
			t.Fatalf("bucket %d not empty after self update: %d", i, n)
		}
	}

	idx := 3
	c := Contact{ID: idWithFirstOneAt(idx), Addr: "peer"}
	rt.Update(c)
	for i, b := range rt.buckets {
		got := b.Contacts()
		if i == idx {
			if len(got) != 1 || got[0].ID != c.ID {
				t.Fatalf("bucket %d contents unexpected: %+v", i, got)
			}
		} else if len(got) != 0 {
			t.Fatalf("bucket %d should be empty, has %d", i, len(got))
		}
	}
}

func TestClosestOrderingAndLimit(t *testing.T) {
	var self id.NodeID
	rt := NewRoutingTable(self)

	id0 := idWithFirstOneAt(0) // 0x80
	id1 := idWithFirstOneAt(1) // 0x40
	id2 := idWithFirstOneAt(2) // 0x20

	rt.Update(Contact{ID: id0, Addr: "id0"})
	rt.Update(Contact{ID: id1, Addr: "id1"})
	rt.Update(Contact{ID: id2, Addr: "id2"})

	got := rt.Closest(id1, 3)
	if len(got) != 3 {
		t.Fatalf("Closest len: got %d want %d", len(got), 3)
	}
	if got[0].ID != id1 || got[1].ID != id2 || got[2].ID != id0 {
		t.Fatalf("Closest order unexpected: %+v", got)
	}

	got2 := rt.Closest(id1, 2)
	if len(got2) != 2 {
		t.Fatalf("Closest limit len: got %d want %d", len(got2), 2)
	}
	if got2[0].ID != id1 || got2[1].ID != id2 {
		t.Fatalf("Closest limit order unexpected: %+v", got2)
	}
}
