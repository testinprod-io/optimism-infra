package service

import (
	"context"
	"errors"
	"net/url"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"

	oprpc "github.com/ethereum-optimism/optimism/op-service/rpc"
	"github.com/ethereum-optimism/optimism/op-service/signer"
)

type ProxyWSService struct {
	wsProxy *WSService
}

type WSService struct {
	logger log.Logger
	sc     *SignerClients
}

func NewProxyWSService(logger log.Logger, sc *SignerClients) *ProxyWSService {
	proxyWSService := WSService{logger, sc}
	return &ProxyWSService{&proxyWSService}
}

func (s *ProxyWSService) RegisterAPIs(server *oprpc.Server) {
	server.AddAPI(rpc.API{
		Namespace: "opsignerproxy",
		Service:   s.wsProxy,
	})
}

func (s *WSService) ServeSigner(ctx context.Context) (bool, error) {
	wsClient, ok := rpc.ClientFromContext(ctx)
	if !ok {
		s.logger.Warn("ws client not provided on opsignerproxy_ServeSigner")
		return false, errors.New("ws client not provided on opsignerproxy_ServeSigner")
	}
	s.sc.wsClients = append(s.sc.wsClients, wsClient)
	return true, nil
}

func NewProxySignerService(logger log.Logger, config SignerServiceConfig, sc *SignerClients) *SignerService {
	ethService := EthProxyService{logger, config, sc}
	opsignerproxy := OpsignerProxyService{logger, config, sc}
	return &SignerService{&ethService, &opsignerproxy}
}

type EthProxyService struct {
	logger log.Logger
	config SignerServiceConfig
	sc     *SignerClients
}

func (s *EthProxyService) SignTransaction(ctx context.Context, args signer.TransactionArgs) (hexutil.Bytes, error) {
	clientName := ""
	u, err := url.Parse(rpc.PeerInfoFromContext(ctx).HTTP.Host)
	if err == nil {
		clientName = u.Hostname()
	} else {
		s.logger.Warn("failed to parse ws peer hostname", "err", err)
	}

	if _, err := s.config.GetAuthConfigForClient(clientName, nil); err != nil {
		return nil, rpc.HTTPError{StatusCode: 403, Status: "Forbidden", Body: []byte(err.Error())}
	}

	var result hexutil.Bytes
	if err := s.sc.Call(ctx, &result, "eth_signTransaction", args); err != nil {
		return nil, rpc.HTTPError{StatusCode: 500, Status: "Proxy Error", Body: []byte(err.Error())}
	}
	return result, nil
}

type OpsignerProxyService struct {
	logger log.Logger
	config SignerServiceConfig
	sc     *SignerClients
}

func (s *OpsignerProxyService) SignBlockPayload(ctx context.Context, args signer.BlockPayloadArgs) (hexutil.Bytes, error) {
	clientName := ""
	u, err := url.Parse(rpc.PeerInfoFromContext(ctx).HTTP.Host)
	if err == nil {
		clientName = u.Hostname()
	} else {
		s.logger.Warn("failed to parse ws peer hostname", "err", err)
	}

	if _, err := s.config.GetAuthConfigForClient(clientName, args.SenderAddress); err != nil {
		return nil, rpc.HTTPError{StatusCode: 403, Status: "Forbidden", Body: []byte(err.Error())}
	}

	var result hexutil.Bytes
	if err := s.sc.Call(ctx, &result, "eth_signTransaction", args); err != nil {
		return nil, rpc.HTTPError{StatusCode: 500, Status: "Proxy Error", Body: []byte(err.Error())}
	}
	return result, nil
}
