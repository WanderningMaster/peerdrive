package routing

import (
	"sync"

	"github.com/WanderningMaster/peerdrive/configuration"
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
