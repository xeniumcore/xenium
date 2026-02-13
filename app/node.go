package app

import (
	"xenium/core"
	"xenium/ports"
)

type Node struct {
	Chain *core.Blockchain
}

func NewNode(cfg Config, clock ports.Clock, logger ports.Logger) *Node {
	return &Node{
		Chain: core.NewBlockchain(cfg.Chain, clock, logger),
	}
}
