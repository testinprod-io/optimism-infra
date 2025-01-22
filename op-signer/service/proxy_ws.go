package service

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

var NoClientsAvailableError = rpc.HTTPError{StatusCode: 500, Status: "Proxy Error", Body: []byte("no clients available")}

type SignerClients struct {
	wsClients []*rpc.Client
	mu        sync.Mutex
}

func (sc *SignerClients) getCopy() []*rpc.Client {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	cc := make([]*rpc.Client, len(sc.wsClients))
	copy(cc, sc.wsClients)
	return cc
}

func (sc *SignerClients) removeClient(c *rpc.Client) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	for i, existing := range sc.wsClients {
		if existing == c {
			sc.wsClients = append(sc.wsClients[:i], sc.wsClients[i+1:]...)
			go c.Close()
			return
		}
	}
}

func (sc *SignerClients) Call(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	localClients := sc.getCopy()
	if len(localClients) == 0 {
		return NoClientsAvailableError
	}

	for _, c := range localClients {
		err := c.CallContext(ctx, result, method, args...)
		var rpcErr rpc.Error
		if err == nil || errors.As(err, &rpcErr) {
			return err
		}

		// if non-RPC error, prevent connection from being re-used
		log.Warn(fmt.Errorf("error during proxy call for %s: %w", method, err).Error())
		sc.removeClient(c)
	}

	return NoClientsAvailableError
}
