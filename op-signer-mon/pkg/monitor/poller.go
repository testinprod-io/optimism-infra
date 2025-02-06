package monitor

import (
	"context"
	"github.com/ethereum-optimism/optimism/op-signer-mon/pkg/config"
	"github.com/ethereum/go-ethereum/log"
)

type Poller struct {
	config     *config.Config
	cancelFunc context.CancelFunc
}

func New(
	config *config.Config) *Poller {
	poller := &Poller{
		config: config,
	}
	return poller
}

func (p *Poller) Start(ctx context.Context) {
	networkCtx, cancelFunc := context.WithCancel(ctx)
	p.cancelFunc = cancelFunc

	schedule(networkCtx, p.config.PingInterval, p.PollPing)
	schedule(networkCtx, p.config.RequestInterval, p.PollRPC)
}

func (p *Poller) Shutdown() {
	if p.cancelFunc != nil {
		p.cancelFunc()
	}
}

func (p *Poller) PollPing(ctx context.Context) {
	log.Debug("poll ping")
	err := p.pollPing(ctx)
	log.Debug("poll ping done", "err", err)
}

func (p *Poller) PollRPC(ctx context.Context) {
	log.Debug("poll rpc")
	err := p.pollRPC(ctx)
	log.Debug("poll rpc done", "err", err)
}
