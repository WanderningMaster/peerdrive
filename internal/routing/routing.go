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

	x := nodeId.XorDist(rt.self, id).Bytes()
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
	return defaults.IdBits - 1
}

func (rt *RoutingTable) Update(c Contact) {
	if c.ID == rt.self {
		return
	}
	idx := rt.BucketIndex(c.ID)
	if ev := rt.buckets[idx].Touch(c); ev != nil {
	}
}

func (rt *RoutingTable) Closest(target nodeId.NodeID, max int) []Contact {
	defaults := configuration.Default()

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
	sort.Slice(all, func(i, j int) bool {
		return nodeId.XorDist(all[i].ID, target).Cmp(nodeId.XorDist(all[j].ID, target)) < 0
	})
	if len(all) > max {
		all = all[:max]
	}
	return all
}

func (rt *RoutingTable) Remove(c Contact) bool {
	idx := rt.BucketIndex(c.ID)
	return rt.buckets[idx].RemoveByID(c.ID)
}

func (rt *RoutingTable) RemoveByAddr(addr string) int {
	removed := 0
	for _, b := range rt.buckets {
		removed += b.RemoveByAddr(addr)
	}
	return removed
}
