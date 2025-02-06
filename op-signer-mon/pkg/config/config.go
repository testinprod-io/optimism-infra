package config

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"math/big"
	"os"
	"time"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"

	_ "gopkg.in/yaml.v3"
)

type Config struct {
	LogLevel string `yaml:"log_level"`

	Metrics MetricsConfig `yaml:"metrics"`
	Healthz HealthzConfig `yaml:"healthz"`

	SignerConfig SignerConfig `yaml:"signer"`
	RPCOptions   RPCOptions   `yaml:"rpc_options"`

	PingInterval    time.Duration `yaml:"ping_interval"`
	RequestInterval time.Duration `yaml:"request_interval"`
}

type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Debug   bool   `yaml:"debug"`
	Host    string `yaml:"host"`
	Port    string `yaml:"port"`
}

type HealthzConfig struct {
	Enabled bool   `yaml:"enabled"`
	Host    string `yaml:"host"`
	Port    string `yaml:"port"`
}

type SignerConfig struct {
	Address   string `yaml:"address"`
	Port      string `yaml:"port"`
	TLSCaCert string `yaml:"tls_ca_cert"`
	TLSCert   string `yaml:"tls_cert"`
	TLSKey    string `yaml:"tls_key"`
}

type RPCOptions struct {
	FromAddress *common.Address `yaml:"from_address"`
	ChainID     *big.Int        `yaml:"chain_id"`
}

func New(file string) (*Config, error) {
	cfg := &Config{}
	contents, err := os.ReadFile(file)
	if err != nil {
		fmt.Printf("error reading config file: %v\n", err)
	}
	if err := yaml.Unmarshal(contents, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	if c.Metrics.Enabled {
		if c.Metrics.Host == "" || c.Metrics.Port == "" {
			return errors.New("metrics is enabled but host or port are missing")
		}
	}
	if c.Healthz.Enabled {
		if c.Healthz.Host == "" || c.Healthz.Port == "" {
			return errors.New("healthz is enabled but host or port are missing")
		}
	}

	if c.LogLevel == "" {
		c.LogLevel = "debug"
	}

	return nil
}
