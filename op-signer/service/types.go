package service

import (
	"errors"

	"github.com/ethereum/go-ethereum/rpc"
)

type ProxyWSClients struct {
	wsClients []*rpc.Client
}

func (pc *ProxyWSClients) GetClient() (*rpc.Client, error) {
	clients := pc.wsClients
	if clients == nil || len(clients) == 0 {
		return nil, errors.New("no clients available")
	}
	return clients[0], nil
}
