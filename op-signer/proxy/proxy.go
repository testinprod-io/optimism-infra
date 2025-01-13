package proxy

import (
	"context"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"

	oprpc "github.com/ethereum-optimism/optimism/op-service/rpc"
	"github.com/ethereum-optimism/optimism/op-service/signer"
)

type SignerProxyService struct {
	opsignerproxy *OpsignerproxyService
}

type OpsignerproxyService struct {
	logger log.Logger
}

func NewSignerProxyService(logger log.Logger) *SignerProxyService {
	opsignerproxyService := OpsignerproxyService{logger}
	return &SignerProxyService{&opsignerproxyService}
}

func (s *SignerProxyService) RegisterAPIs(server *oprpc.Server) {
	server.AddAPI(rpc.API{
		Namespace: "opsignerproxy",
		Service:   s.opsignerproxy,
	})
}

func (s *OpsignerproxyService) ServeSigner(ctx context.Context, args signer.TransactionArgs) (bool, error) {
	return false, nil
}
