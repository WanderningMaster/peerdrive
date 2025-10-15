package routing

import (
	"sort"

	"github.com/WanderningMaster/peerdrive/configuration"
	nodeId "github.com/WanderningMaster/peerdrive/internal/id"
)

type RoutingTable struct {
    self    nodeId.NodeID
    buckets []*Bucket
}

func NewRoutingTable(self nodeId.NodeID) *RoutingTable {
	defaults := configuration.Default()
	rt := &RoutingTable{self: self}

	rt.buckets = make([]*Bucket, defaults.IdBits)
	for i := 0; i < defaults.IdBits; i++ {
		rt.buckets[i] = &Bucket{}
	}
	return rt
}

func (rt *RoutingTable) BucketIndex(id nodeId.NodeID) int {
	defaults := configuration.Default()

	// index = leading zeros in xor(self, id)
	x := nodeId.XorDist(rt.self, id).Bytes()
	// Convert to 256-bit padded
	var buf [32]byte
	copy(buf[32-len(x):], x)
	// count leading zeros
	lz := 0
	for _, b := range buf {
		for i := 7; i >= 0; i-- {
			if (b>>uint(i))&1 == 0 {
				lz++
			} else {
				return lz
			}
		}
	}
	return defaults.IdBits - 1 // identical ID (shouldn't happen), put in last bucket
}

func (rt *RoutingTable) Update(c Contact) {
	if c.ID == rt.self {
		return
	}
	idx := rt.BucketIndex(c.ID)
	if ev := rt.buckets[idx].Touch(c); ev != nil {
		// Eviction policy: drop silently (classic Kademlia would ping evicted)
	}
}

func (rt *RoutingTable) Closest(target nodeId.NodeID, max int) []Contact {
	defaults := configuration.Default()

	// collect from buckets near index
	idx := rt.BucketIndex(target)
	var all []Contact
	for radius := 0; len(all) < max && (idx-radius >= 0 || idx+radius < defaults.IdBits); radius++ {
		if idx-radius >= 0 {
			all = append(all, rt.buckets[idx-radius].Contacts()...)
		}
		if idx+radius < defaults.IdBits && radius != 0 {
			all = append(all, rt.buckets[idx+radius].Contacts()...)
		}
	}
	// sort by XOR distance
	sort.Slice(all, func(i, j int) bool {
		return nodeId.XorDist(all[i].ID, target).Cmp(nodeId.XorDist(all[j].ID, target)) < 0
	})
	if len(all) > max {
		all = all[:max]
	}
	return all
}

// Remove removes the given contact from its bucket (by ID). Returns true if removed.
func (rt *RoutingTable) Remove(c Contact) bool {
    idx := rt.BucketIndex(c.ID)
    return rt.buckets[idx].RemoveByID(c.ID)
}

// RemoveByAddr removes any contacts matching the address across all buckets.
// Returns the count of removed contacts.
func (rt *RoutingTable) RemoveByAddr(addr string) int {
    removed := 0
    for _, b := range rt.buckets {
        removed += b.RemoveByAddr(addr)
    }
    return removed
}
