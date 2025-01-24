package service

import (
	"errors"
	opservice "github.com/ethereum-optimism/optimism/op-service"
	"github.com/urfave/cli/v2"
	"math"
)

const (
	EnableProxyFlagName   = "proxy.enabled"
	ListenAddrFlagName    = "proxy.addr"
	PortFlagName          = "proxy.port"
	SignerCACertFlagName  = "proxy.signer.ca"
	SignerNameFlagName    = "proxy.signer.name"
	SignerTlsCertFlagName = "proxy.signer.tls.cert"
	SignerTlsKeyFlagName  = "proxy.signer.tls.key"
)

var ErrInvalidPort = errors.New("invalid RPC port")
var ErrMissingSignerConfig = errors.New("all signer mTLS config must be set")

func CLIFlags(envPrefix string) []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{
			Name:    EnableProxyFlagName,
			Usage:   "Run in proxy mode",
			EnvVars: opservice.PrefixEnvVar(envPrefix, "PROXY_ENABLED"),
		},
		&cli.StringFlag{
			Name:    ListenAddrFlagName,
			Usage:   "op-signer proxy listening address",
			Value:   "0.0.0.0", // TODO: Switch to 127.0.0.1
			EnvVars: opservice.PrefixEnvVar(envPrefix, "PROXY_ADDR"),
		},
		&cli.IntFlag{
			Name:    PortFlagName,
			Usage:   "op-signer proxy listening port",
			Value:   9989,
			EnvVars: opservice.PrefixEnvVar(envPrefix, "PROXY_PORT"),
		},
		&cli.StringFlag{
			Name:    SignerCACertFlagName,
			Usage:   "op-signer CA certificate file path, for mTLS",
			Value:   "",
			EnvVars: opservice.PrefixEnvVar(envPrefix, "PROXY_SIGNER_CA"),
		},
		&cli.StringFlag{
			Name:    SignerNameFlagName,
			Usage:   "op-signer domain name, for mTLS",
			Value:   "",
			EnvVars: opservice.PrefixEnvVar(envPrefix, "PROXY_SIGNER_NAME"),
		},
		&cli.StringFlag{
			Name:    SignerTlsCertFlagName,
			Usage:   "op-signer TLS certificate file path, for TLS",
			Value:   "",
			EnvVars: opservice.PrefixEnvVar(envPrefix, "PROXY_SIGNER_TLS_CERT"),
		},
		&cli.StringFlag{
			Name:    SignerTlsKeyFlagName,
			Usage:   "op-signer TLS private key file path, for TLS",
			Value:   "",
			EnvVars: opservice.PrefixEnvVar(envPrefix, "PROXY_SIGNER_TLS_KEY"),
		},
	}
}

type ProxyCLIConfig struct {
	EnableProxy bool
	ListenAddr  string
	ListenPort  int
	SignerCA    string
	SignerName  string
	TLSCert     string
	TLSKey      string
}

func (c ProxyCLIConfig) Check() error {
	if !c.EnableProxy {
		return nil
	}

	if c.ListenPort < 0 || c.ListenPort > math.MaxUint16 {
		return ErrInvalidPort
	}

	if c.SignerCA == "" || c.SignerName == "" || c.TLSCert == "" || c.TLSKey == "" {
		return ErrMissingSignerConfig
	}

	return nil
}

func ReadProxyCLIConfig(ctx *cli.Context) ProxyCLIConfig {
	return ProxyCLIConfig{
		EnableProxy: ctx.Bool(EnableProxyFlagName),
		ListenAddr:  ctx.String(ListenAddrFlagName),
		ListenPort:  ctx.Int(PortFlagName),
		SignerCA:    ctx.String(SignerCACertFlagName),
		SignerName:  ctx.String(SignerNameFlagName),
		TLSCert:     ctx.String(SignerTlsCertFlagName),
		TLSKey:      ctx.String(SignerTlsKeyFlagName),
	}
}
