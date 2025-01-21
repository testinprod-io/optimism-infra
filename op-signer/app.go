package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/urfave/cli/v2"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/ethereum-optimism/optimism/op-service/cliapp"
	"github.com/ethereum-optimism/optimism/op-service/httputil"
	oplog "github.com/ethereum-optimism/optimism/op-service/log"
	opmetrics "github.com/ethereum-optimism/optimism/op-service/metrics"
	"github.com/ethereum-optimism/optimism/op-service/oppprof"
	oprpc "github.com/ethereum-optimism/optimism/op-service/rpc"
	"github.com/ethereum-optimism/optimism/op-service/tls/certman"

	"github.com/ethereum-optimism/infra/op-signer/client"
	"github.com/ethereum-optimism/infra/op-signer/service"
)

type SignerApp struct {
	log log.Logger

	version string

	pprofServer   *oppprof.Service
	metricsServer *httputil.HTTPServer
	registry      *prometheus.Registry

	signer *service.SignerService

	rpc *oprpc.Server

	wsProxy        *oprpc.Server
	wsProxyService *service.ProxyWSService

	stopped atomic.Bool
}

func InitFromConfig(ctx context.Context, log log.Logger, cfg *Config, version string) (*SignerApp, error) {
	if err := cfg.Check(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	app := &SignerApp{log: log, version: version}
	if err := app.init(cfg); err != nil {
		return nil, errors.Join(err, app.Stop(ctx)) // clean up the failed init attempt
	}
	return app, nil
}

func (s *SignerApp) init(cfg *Config) error {
	if err := s.initPprof(cfg); err != nil {
		return fmt.Errorf("pprof error: %w", err)
	}
	if err := s.initMetrics(cfg); err != nil {
		return fmt.Errorf("metrics error: %w", err)
	}
	if cfg.ProxyConfig.EnableProxy {
		if err := s.initProxy(cfg); err != nil {
			return fmt.Errorf("proxy error: %w", err)
		}
	} else {
		if err := s.initRPC(cfg); err != nil {
			return fmt.Errorf("metrics error: %w", err)
		}
	}
	return nil
}

func (s *SignerApp) initPprof(cfg *Config) error {
	if !cfg.PprofConfig.ListenEnabled {
		return nil
	}
	s.pprofServer = oppprof.New(
		cfg.PprofConfig.ListenEnabled,
		cfg.PprofConfig.ListenAddr,
		cfg.PprofConfig.ListenPort,
		cfg.PprofConfig.ProfileType,
		cfg.PprofConfig.ProfileDir,
		cfg.PprofConfig.ProfileFilename,
	)
	s.log.Info("Starting pprof server", "addr", cfg.PprofConfig.ListenAddr, "port", cfg.PprofConfig.ListenPort)
	if err := s.pprofServer.Start(); err != nil {
		return fmt.Errorf("failed to start pprof server: %w", err)
	}
	return nil
}

func (s *SignerApp) initMetrics(cfg *Config) error {
	registry := opmetrics.NewRegistry()
	registry.MustRegister(service.MetricSignTransactionTotal)
	s.registry = registry // some things require metrics registry

	if !cfg.MetricsConfig.Enabled {
		return nil
	}

	metricsCfg := cfg.MetricsConfig
	s.log.Info("Starting metrics server", "addr", metricsCfg.ListenAddr, "port", metricsCfg.ListenPort)
	metricsServer, err := opmetrics.StartServer(registry, metricsCfg.ListenAddr, metricsCfg.ListenPort)
	if err != nil {
		return fmt.Errorf("failed to start metrics server: %w", err)
	}
	s.log.Info("Started metrics server", "endpoint", metricsServer.Addr())
	s.metricsServer = metricsServer
	return nil
}

func (s *SignerApp) initRPC(cfg *Config) error {
	caCert, err := os.ReadFile(cfg.TLSConfig.TLSCaCert)
	if err != nil {
		return fmt.Errorf("failed to read tls ca cert: %s", string(caCert))
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	cm, err := certman.New(s.log, cfg.TLSConfig.TLSCert, cfg.TLSConfig.TLSKey)
	if err != nil {
		return fmt.Errorf("failed to read tls cert or key: %w", err)
	}
	if err := cm.Watch(); err != nil {
		return fmt.Errorf("failed to start certman watcher: %w", err)
	}

	tlsConfig := &tls.Config{
		GetCertificate: cm.GetCertificate,
		ClientCAs:      caCertPool,
		ClientAuth:     tls.VerifyClientCertIfGiven, // necessary for k8s healthz probes, but we check the cert in service/auth.go
	}
	serverTlsConfig := &oprpc.ServerTLSConfig{
		Config:    tlsConfig,
		CLIConfig: &cfg.TLSConfig,
	}

	rpcCfg := cfg.RPCConfig
	s.rpc = oprpc.NewServer(
		rpcCfg.ListenAddr,
		rpcCfg.ListenPort,
		s.version,
		oprpc.WithLogger(s.log),
		oprpc.WithTLSConfig(serverTlsConfig),
		oprpc.WithMiddleware(service.NewAuthMiddleware()),
		oprpc.WithHTTPRecorder(opmetrics.NewPromHTTPRecorder(s.registry, "signer")),
	)

	serviceCfg, err := service.ReadConfig(cfg.ServiceConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read service config: %w", err)
	}
	s.signer = service.NewSignerService(s.log, serviceCfg)
	s.signer.RegisterAPIs(s.rpc)

	if err := s.rpc.Start(); err != nil {
		return fmt.Errorf("error starting RPC server: %w", err)
	}
	s.log.Info("Started op-signer RPC server", "addr", s.rpc.Endpoint())

	if len(serviceCfg.Proxy) > 0 {
		proxyTLSConfig := &tls.Config{
			GetClientCertificate: cm.GetClientCertificate,
		}
		s.ConnectProxy(serviceCfg, proxyTLSConfig)
	}

	return nil
}

func (s *SignerApp) initProxy(cfg *Config) error {
	sc := &service.SignerClients{}

	// init ws server for op-signer
	// client (op-signer) CA must be trusted
	wsCaCert, err := os.ReadFile(cfg.ProxyConfig.SignerCA)
	if err != nil {
		return fmt.Errorf("failed to read tls ca cert: %s", string(wsCaCert))
	}
	wsCaCertPool := x509.NewCertPool()
	wsCaCertPool.AppendCertsFromPEM(wsCaCert)

	wsCm, err := certman.New(s.log, cfg.TLSConfig.TLSCert, cfg.TLSConfig.TLSKey)
	if err != nil {
		return fmt.Errorf("failed to read tls cert or key: %w", err)
	}
	if err := wsCm.Watch(); err != nil {
		return fmt.Errorf("failed to start certman watcher: %w", err)
	}

	wsTlsConfig := &tls.Config{
		GetCertificate: wsCm.GetCertificate,
		ClientCAs:      wsCaCertPool,
		ClientAuth:     tls.VerifyClientCertIfGiven, // necessary for k8s healthz probes, but we check the cert in service/auth.go
	}
	wsServerTlsConfig := &oprpc.ServerTLSConfig{
		Config: wsTlsConfig,
	}

	wsProxyConfig := cfg.ProxyConfig
	s.wsProxy = oprpc.NewServer(
		wsProxyConfig.ListenAddr,
		wsProxyConfig.ListenPort,
		s.version,
		oprpc.WithWebsocketEnabled(),
		oprpc.WithLogger(s.log),
		oprpc.WithTLSConfig(wsServerTlsConfig),
		oprpc.WithMiddleware(service.NewAuthMiddleware()),
		oprpc.WithHTTPRecorder(opmetrics.NewPromHTTPRecorder(s.registry, "wsproxy")),
	)
	s.wsProxyService = service.NewProxyWSService(s.log, sc)
	s.wsProxyService.RegisterAPIs(s.wsProxy)

	if err := s.wsProxy.Start(); err != nil {
		return fmt.Errorf("error starting proxy RPC(WS) server: %w", err)
	}
	s.log.Info("Started op-signer proxy RPC(WS) server", "addr", s.wsProxy.Endpoint())

	// init RPC proxy server
	caCert, err := os.ReadFile(cfg.TLSConfig.TLSCaCert)
	if err != nil {
		return fmt.Errorf("failed to read tls ca cert: %s", string(caCert))
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	cm, err := certman.New(s.log, cfg.TLSConfig.TLSCert, cfg.TLSConfig.TLSKey)
	if err != nil {
		return fmt.Errorf("failed to read tls cert or key: %w", err)
	}
	if err := cm.Watch(); err != nil {
		return fmt.Errorf("failed to start certman watcher: %w", err)
	}

	tlsConfig := &tls.Config{
		GetCertificate: cm.GetCertificate,
		ClientCAs:      caCertPool,
		ClientAuth:     tls.VerifyClientCertIfGiven, // necessary for k8s healthz probes, but we check the cert in service/auth.go
	}
	serverTlsConfig := &oprpc.ServerTLSConfig{
		Config:    tlsConfig,
		CLIConfig: &cfg.TLSConfig,
	}

	rpcCfg := cfg.RPCConfig
	s.rpc = oprpc.NewServer(
		rpcCfg.ListenAddr,
		rpcCfg.ListenPort,
		s.version,
		oprpc.WithLogger(s.log),
		oprpc.WithTLSConfig(serverTlsConfig),
		oprpc.WithMiddleware(service.NewAuthMiddleware()),
		oprpc.WithHTTPRecorder(opmetrics.NewPromHTTPRecorder(s.registry, "signerproxy")),
	)

	serviceCfg, err := service.ReadConfig(cfg.ServiceConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read service config: %w", err)
	}
	s.signer = service.NewProxySignerService(s.log, serviceCfg, sc)
	s.signer.RegisterAPIs(s.rpc)

	if err := s.rpc.Start(); err != nil {
		return fmt.Errorf("error starting RPC server: %w", err)
	}
	s.log.Info("Started op-signer RPC server", "addr", s.rpc.Endpoint())

	return nil
}

func (s *SignerApp) ConnectProxy(cfg service.SignerServiceConfig, tlsConfig *tls.Config) {
	for _, pc := range cfg.Proxy {
		if pc.Enable {
			go func() {
				ctx := context.Background()
				dialer := websocket.Dialer{
					Proxy:           http.ProxyFromEnvironment,
					TLSClientConfig: tlsConfig,
				}
				c, err := rpc.DialOptions(ctx, pc.ProxyEndpoint, rpc.WithWebsocketDialer(dialer))
				if err != nil {
					s.log.Warn("Failed to connect to proxy", "name", pc.ProxyEndpoint, "err", err)
					return
				}

				for _, api := range s.signer.ListAPIs() {
					if err := c.RegisterName(api.Namespace, api.Service); err != nil {
						s.log.Warn("Failed to register api for proxy websocket", "name", api.Namespace, "err", err)
						c.Close()
						return
					}
				}

				var result bool
				if err := c.CallContext(ctx, &result, "opsignerproxy_serveSigner"); err != nil {
					s.log.Warn("Failed to establish signer-proxy connection", "err", err)
					c.Close()
					return
				} else if !result {
					s.log.Warn("Failed to establish signer-proxy connection with server error")
					c.Close()
					return
				}
				s.log.Info("Connected to op-signer proxy server", "endpoint", pc.ProxyEndpoint)
			}()
		}
	}
}

func (s *SignerApp) Start(ctx context.Context) error {
	return nil
}

func (s *SignerApp) Stop(ctx context.Context) error {
	var result error
	if s.rpc != nil {
		if err := s.rpc.Stop(); err != nil {
			result = errors.Join(result, fmt.Errorf("failed to stop RPC server: %w", err))
		}
	}
	if s.pprofServer != nil {
		if err := s.pprofServer.Stop(ctx); err != nil {
			result = errors.Join(result, fmt.Errorf("failed to stop pprof server: %w", err))
		}
	}
	if s.metricsServer != nil {
		if err := s.metricsServer.Stop(ctx); err != nil {
			result = errors.Join(result, fmt.Errorf("failed to stop metrics server: %w", err))
		}
	}
	return result
}

func (s *SignerApp) Stopped() bool {
	return s.stopped.Load()
}

var _ cliapp.Lifecycle = (*SignerApp)(nil)

func MainAppAction(version string) cliapp.LifecycleAction {
	return func(cliCtx *cli.Context, _ context.CancelCauseFunc) (cliapp.Lifecycle, error) {
		cfg := NewConfig(cliCtx)
		logger := oplog.NewLogger(cliCtx.App.Writer, cfg.LogConfig)
		return InitFromConfig(cliCtx.Context, logger, cfg, version)
	}
}

type SignActionType string

const (
	SignTransaction  SignActionType = "transaction"
	SignBlockPayload SignActionType = "block_payload"
)

func ClientSign(version string, action SignActionType) func(cliCtx *cli.Context) error {
	return func(cliCtx *cli.Context) error {
		cfg := NewConfig(cliCtx)
		if err := cfg.Check(); err != nil {
			return fmt.Errorf("invalid CLI flags: %w", err)
		}

		l := oplog.NewLogger(os.Stdout, cfg.LogConfig)
		oplog.SetGlobalLogHandler(l.Handler())

		switch action {
		case SignTransaction:
			txarg := cliCtx.Args().Get(0)
			if txarg == "" {
				return errors.New("no transaction argument was provided")
			}
			txraw, err := hexutil.Decode(txarg)
			if err != nil {
				return errors.New("failed to decode transaction argument")
			}

			client, err := client.NewSignerClient(l, cfg.ClientEndpoint, cfg.TLSConfig)
			if err != nil {
				return err
			}

			tx := &types.Transaction{}
			if err := tx.UnmarshalBinary(txraw); err != nil {
				return fmt.Errorf("failed to unmarshal transaction argument: %w", err)
			}

			tx, err = client.SignTransaction(context.Background(), tx)
			if err != nil {
				return err
			}

			result, _ := tx.MarshalJSON()
			fmt.Println(string(result))

		case SignBlockPayload:
			blockPayloadHash := cliCtx.Args().Get(0)
			if blockPayloadHash == "" {
				return errors.New("no block payload argument was provided")
			}

			client, err := client.NewSignerClient(l, cfg.ClientEndpoint, cfg.TLSConfig)
			if err != nil {
				return err
			}

			signingHash := common.Hash{}
			if err := signingHash.UnmarshalText([]byte(blockPayloadHash)); err != nil {
				return fmt.Errorf("failed to unmarshal block payload argument: %w", err)
			}

			signature, err := client.SignBlockPayload(context.Background(), signingHash)
			if err != nil {
				return err
			}

			fmt.Println(string(signature[:]))

		case "":
			return errors.New("no action was provided")
		}

		return nil
	}
}
