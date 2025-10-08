package routing

import "github.com/WanderningMaster/peerdrive/internal/id"

type Contact struct {
	ID   id.NodeID `json:"id"`
	Addr string    `json:"addr"`
}
