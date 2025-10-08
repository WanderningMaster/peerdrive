package rpc

import "github.com/WanderningMaster/peerdrive/internal/routing"

type RpcType string

const (
	Ping      RpcType = "PING"
	Store     RpcType = "STORE"
	FindNode  RpcType = "FIND_NODE"
	FindValue RpcType = "FIND_VALUE"
)

type RpcMessage struct {
	Type  RpcType           `json:"type"`
	From  routing.Contact   `json:"from"`
	Key   string            `json:"key,omitempty"`
	Value []byte            `json:"value,omitempty"`
	Nodes []routing.Contact `json:"nodes,omitempty"`
	Found bool              `json:"found,omitempty"`
}
