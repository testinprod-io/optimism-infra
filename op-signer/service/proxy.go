package service

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/gorilla/websocket"

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
	s.sc.AddClient(wsClient)
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
	clientInfo := ClientInfoFromContext(ctx)
	if _, err := s.config.GetAuthConfigForClient(clientInfo.ClientName, nil); err != nil {
		return nil, rpc.HTTPError{StatusCode: 403, Status: "Forbidden", Body: []byte(err.Error())}
	}

	var result hexutil.Bytes
	if err := s.sc.Call(ctx, &result, "eth_signTransaction", args); err != nil {
		return nil, err
	}
	return result, nil
}

type OpsignerProxyService struct {
	logger log.Logger
	config SignerServiceConfig
	sc     *SignerClients
}

func (s *OpsignerProxyService) SignBlockPayload(ctx context.Context, args signer.BlockPayloadArgs) (hexutil.Bytes, error) {
	clientInfo := ClientInfoFromContext(ctx)
	if _, err := s.config.GetAuthConfigForClient(clientInfo.ClientName, args.SenderAddress); err != nil {
		return nil, rpc.HTTPError{StatusCode: 403, Status: "Forbidden", Body: []byte(err.Error())}
	}

	var result hexutil.Bytes
	if err := s.sc.Call(ctx, &result, "opsigner_signBlockPayload", args); err != nil {
		return nil, err
	}
	return result, nil
}

type ProxyClient struct {
	ctx       context.Context
	endpoint  string
	tlsConfig *tls.Config
	apis      *[]rpc.API

	client              *rpc.Client
	connRetryMaxBackoff time.Duration
	pingInterval        time.Duration

	done chan struct{}
}

const (
	wsMaxBackoff        = 5 * time.Minute
	wsReadinessInterval = time.Minute
)

func NewProxyClient(ctx context.Context, endpoint string, tlsConfig *tls.Config, apis *[]rpc.API) *ProxyClient {
	p := ProxyClient{
		ctx:                 ctx,
		endpoint:            endpoint,
		tlsConfig:           tlsConfig,
		apis:                apis,
		connRetryMaxBackoff: wsMaxBackoff,
		pingInterval:        wsReadinessInterval,
		done:                make(chan struct{}),
	}
	return &p
}

func (p *ProxyClient) Start() {
	for {
		select {
		case <-p.done:
			return
		default:
		}

		// connect (or re-connect)
		if err := p.connectWithBackoff(); err != nil {
			log.Warn("Failed to connect to proxy", "err", err)
			p.done <- struct{}{}
			return
		}
		log.Info("Connected to op-signer proxy server", "endpoint", p.endpoint)

		// wait until error occurs on the connection, only returns when loop is broken
		err := p.serveSignerLoop()
		if err != nil {
			log.Warn("WebSocket conn to proxy terminated", "err", err)
		}
	}
}

func (p *ProxyClient) connectWithBackoff() error {
	dialer := websocket.Dialer{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: p.tlsConfig,
	}

	dial := func() (*rpc.Client, error) {
		c, err := rpc.DialOptions(p.ctx, p.endpoint, rpc.WithWebsocketDialer(dialer))
		if err != nil {
			return nil, fmt.Errorf("ws connection failed: %w, endpoint: %s", err, p.endpoint)
		}
		return c, nil
	}

	notify := func(err error, t time.Duration) {
		log.Warn(fmt.Sprintf("WebSocket connection failed, retrying in %s", t), "err", err)
	}

	b := backoff.NewExponentialBackOff()
	b.MaxInterval = p.connRetryMaxBackoff
	c, err := backoff.Retry(
		context.Background(),
		dial,
		backoff.WithBackOff(b),
		backoff.WithNotify(notify),
		backoff.WithMaxElapsedTime(0), // infinite
	)
	if err != nil {
		return err
	}

	// set to service RPC methods
	for _, api := range *p.apis {
		if err := c.RegisterName(api.Namespace, api.Service); err != nil {
			c.Close()
			return fmt.Errorf("api registration failed: %w", err)
		}
	}

	p.client = c
	return nil
}

func (p *ProxyClient) serveSignerLoop() error {
	timer := time.NewTimer(0)
	defer p.client.Close()
	defer timer.Stop()

	isFirstCall := true
	for {
		select {
		case <-p.done:
			return nil
		case <-timer.C:
			var result bool
			if err := p.client.CallContext(p.ctx, &result, "opsignerproxy_serveSigner"); err != nil {
				p.client.Close()
				return fmt.Errorf("failed to register as signer on proxy: %w", err)
			}
			if isFirstCall {
				isFirstCall = false
				log.Info("Serving RPC on WS to proxy", "endpoint", p.endpoint)
			}
			timer.Reset(p.pingInterval)
		}
	}
}
