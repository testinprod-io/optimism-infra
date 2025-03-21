package config

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestConfig(t *testing.T) {
	t.Run("should load an example config file", func(t *testing.T) {
		config, err := New("../../config.example.yaml")
		require.NoError(t, err)
		require.NotNil(t, config)

		require.Equal(t, "debug", config.LogLevel)

		require.Equal(t, false, config.Metrics.Debug)
		require.Equal(t, true, config.Metrics.Enabled)
		require.Equal(t, "0.0.0.0", config.Metrics.Host)
		require.Equal(t, "7300", config.Metrics.Port)

		require.Equal(t, true, config.Healthz.Enabled)
		require.Equal(t, "0.0.0.0", config.Healthz.Host)
		require.Equal(t, "8080", config.Healthz.Port)

		require.Equal(t, mustParseDuration("5s"), config.PingInterval)
		require.Equal(t, mustParseDuration("5m"), config.RequestInterval)

		require.Equal(t, "http://localhost", config.SignerConfig.Address)
		require.Equal(t, "8080", config.SignerConfig.Port)
		require.Equal(t, "tls/ca.crt", config.SignerConfig.TLSCaCert)
		require.Equal(t, "tls/tls.crt", config.SignerConfig.TLSCert)
		require.Equal(t, "tls/tls.key", config.SignerConfig.TLSKey)

		require.Equal(t, common.HexToAddress("0xaD4796D21431A3d4272A681CE67B0236f1A5783A"), *config.RPCOptions.FromAddress)
		require.Equal(t, big.NewInt(901), config.RPCOptions.ChainID)

		require.NoError(t, config.Validate())
	})
}

func mustParseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		panic(err)
	}
	return d
}
