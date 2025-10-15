package routing

import (
    "sync"

    "github.com/WanderningMaster/peerdrive/configuration"
    "github.com/WanderningMaster/peerdrive/internal/id"
)

type Bucket struct {
    mu   sync.Mutex
    list []Contact // most-recently seen at end
}

func (b *Bucket) Touch(c Contact) (evicted *Contact) {
	defaults := configuration.Default()

	b.mu.Lock()
	defer b.mu.Unlock()

	// move to end if exists
	for i := range b.list {
		if b.list[i].ID == c.ID {
			v := b.list[i]
			b.list = append(append([]Contact{}, b.list[:i]...), b.list[i+1:]...)
			b.list = append(b.list, v)
			return nil
		}
	}
	if len(b.list) < defaults.KBucketK {
		b.list = append(b.list, c)
		return nil
	}
	// evict LRU (front)
	old := b.list[0]
	b.list = append(b.list[1:], c)
	return &old
}

func (b *Bucket) Contacts() []Contact {
    b.mu.Lock()
    defer b.mu.Unlock()

    out := make([]Contact, len(b.list))
    copy(out, b.list)
    return out
}

// RemoveByID removes a contact with matching ID from the bucket, if present.
// Returns true if a contact was removed.
func (b *Bucket) RemoveByID(nid id.NodeID) bool {
    b.mu.Lock()
    defer b.mu.Unlock()
    for i := range b.list {
        if b.list[i].ID == nid {
            b.list = append(b.list[:i], b.list[i+1:]...)
            return true
        }
    }
    return false
}

// RemoveByAddr removes any contacts with the given address. Returns number removed.
func (b *Bucket) RemoveByAddr(addr string) int {
    b.mu.Lock()
    defer b.mu.Unlock()
    removed := 0
    // Compact in-place
    j := 0
    for i := 0; i < len(b.list); i++ {
        if b.list[i].Addr == addr {
            removed++
            continue
        }
        if j != i {
            b.list[j] = b.list[i]
        }
        j++
    }
    if removed > 0 {
        b.list = b.list[:j]
    }
    return removed
}
