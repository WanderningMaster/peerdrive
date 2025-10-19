package relay

import (
	"github.com/WanderningMaster/peerdrive/internal/rpc"
)

type FrameType string

const (
	Register        FrameType = "REGISTER"
	Whoami          FrameType = "WHOAMI"
	DeliverRequest  FrameType = "DELIVER_REQUEST"
	DeliverResponse FrameType = "DELIVER_RESPONSE"
	ClientRequest   FrameType = "CLIENT_REQUEST"
	ClientResponse  FrameType = "CLIENT_RESPONSE"
)

type Frame struct {
	Type     FrameType      `json:"type"`
	ReqID    string         `json:"reqId,omitempty"`
	TargetID string         `json:"targetId,omitempty"`
	Payload  rpc.RpcMessage `json:"payload,omitempty"`
	Error    string         `json:"error,omitempty"`
}
