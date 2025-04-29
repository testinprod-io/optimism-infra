package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

var NoClientsAvailableError = rpc.HTTPError{StatusCode: 500, Status: "Proxy Error", Body: []byte("no clients available")}

type SignerClients struct {
	wsClients map[*rpc.Client]struct{}
}

func NewSignerClients() *SignerClients {
	return &SignerClients{make(map[*rpc.Client]struct{})}
}

func (sc *SignerClients) AddClient(c *rpc.Client) {
	sc.wsClients[c] = struct{}{}
}

func (sc *SignerClients) getKeys() []*rpc.Client {
	keys := make([]*rpc.Client, 0, len(sc.wsClients))
	for k := range sc.wsClients {
		keys = append(keys, k)
	}
	return keys
}

func (sc *SignerClients) Call(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	for _, c := range sc.getKeys() {
		if _, ok := sc.wsClients[c]; !ok {
			continue
		}

		err := c.CallContext(ctx, result, method, args...)
		var rpcErr rpc.Error
		if err == nil || errors.As(err, &rpcErr) {
			return err
		}

		// if non-RPC error, prevent connection from being re-used
		log.Warn(fmt.Errorf("error during proxy call for %s: %w", method, err).Error())
		c.Close()
		delete(sc.wsClients, c)
	}

	return NoClientsAvailableError
}
