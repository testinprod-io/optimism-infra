package service

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"

	oprpc "github.com/ethereum-optimism/optimism/op-service/rpc"
)

type SignerProxyService struct {
	opsignerproxy *OpsignerproxyService
}

type OpsignerproxyService struct {
	logger log.Logger

	pc *ProxyWSClients
}

func NewSignerProxyService(logger log.Logger, proxyClients *ProxyWSClients) *SignerProxyService {
	opsignerproxyService := OpsignerproxyService{logger, proxyClients}
	return &SignerProxyService{&opsignerproxyService}
}

func (s *SignerProxyService) RegisterAPIs(server *oprpc.Server) {
	server.AddAPI(rpc.API{
		Namespace: "opsignerproxy",
		Service:   s.opsignerproxy,
	})
}

func (s *OpsignerproxyService) ServeSigner(ctx context.Context) (bool, error) {
	wsClient, ok := rpc.ClientFromContext(ctx)
	if !ok {
		s.logger.Warn("ws client not provided on opsignerproxy_ServeSigner")
		return false, errors.New("ws client not provided on opsignerproxy_ServeSigner")
	}
	s.pc.wsClients = append(s.pc.wsClients, wsClient)
	return true, nil
}
