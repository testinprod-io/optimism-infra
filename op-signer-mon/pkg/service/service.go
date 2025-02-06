package service

import (
	"context"
	"errors"

	"github.com/ethereum-optimism/optimism/op-signer-mon/pkg/config"
	"github.com/ethereum-optimism/optimism/op-signer-mon/pkg/metrics"
	"github.com/ethereum-optimism/optimism/op-signer-mon/pkg/monitor"

	"github.com/ethereum/go-ethereum/log"
	"net"
	"net/http"
)

type Service struct {
	Config  *config.Config
	Healthz *HealthzServer
	Metrics *MetricsServer
}

func New(cfg *config.Config) *Service {
	s := &Service{
		Config:  cfg,
		Healthz: &HealthzServer{},
		Metrics: &MetricsServer{},
	}
	return s
}

func (s *Service) Start(ctx context.Context) {
	log.Info("service starting")
	if s.Config.Healthz.Enabled {
		addr := net.JoinHostPort(s.Config.Healthz.Host, s.Config.Healthz.Port)
		log.Info("starting healthz server",
			"addr", addr)
		go func() {
			if err := s.Healthz.Start(ctx, addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("error starting healthz server",
					"err", err)
			}
		}()
	}

	metrics.Debug = s.Config.Metrics.Debug
	if s.Config.Metrics.Enabled {
		addr := net.JoinHostPort(s.Config.Metrics.Host, s.Config.Metrics.Port)
		log.Info("starting metrics server",
			"addr", addr)
		go func() {
			if err := s.Metrics.Start(ctx, addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("error starting metrics server",
					"err", err)
			}
		}()
	}

	n := monitor.New(s.Config)
	n.Start(ctx)

	log.Info("service started")
}

func (s *Service) Shutdown() {
	log.Info("service shutting down")
	if s.Config.Healthz.Enabled {
		err := s.Healthz.Shutdown()
		if err != nil {
			log.Warn("failed to shutdown healthz server: %v", err)
		}
		log.Info("healthz stopped")
	}
	if s.Config.Metrics.Enabled {
		err := s.Metrics.Shutdown()
		if err != nil {
			log.Warn("failed to shutdown metrics server: %v", err)
		}
		log.Info("metrics stopped")
	}
	log.Info("service stopped")
}
