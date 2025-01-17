package service

import (
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"gopkg.in/yaml.v3"
)

type AuthConfig struct {
	// ClientName DNS name of the client connecting to op-signer.
	ClientName string `yaml:"name"`
	// KeyName key resource name of the Cloud KMS
	KeyName string `yaml:"key"`
	// ChainID chain id of the op-signer to sign for
	ChainID uint64 `yaml:"chainID"`
	// FromAddress sender address that is sending the rpc request
	FromAddress common.Address `yaml:"fromAddress"`
	ToAddresses []string       `yaml:"toAddresses"`
	MaxValue    string         `yaml:"maxValue"`
}

func (c AuthConfig) MaxValueToInt() *big.Int {
	return hexutil.MustDecodeBig(c.MaxValue)
}

type ProxyConfig struct {
	ProxyEndpoint string `yaml:"proxyEndpoint"`
	Enable        bool   `yaml:"enable"`
}

type SignerServiceConfig struct {
	Auth  []AuthConfig  `yaml:"auth"`
	Proxy []ProxyConfig `yaml:"proxy"`
}

func ReadConfig(path string) (SignerServiceConfig, error) {
	config := SignerServiceConfig{}
	data, err := os.ReadFile(path)
	if err != nil {
		return config, err
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return config, err
	}
	for _, authConfig := range config.Auth {
		for _, toAddress := range authConfig.ToAddresses {
			if _, err := hexutil.Decode(toAddress); err != nil {
				return config, fmt.Errorf("invalid toAddress '%s' in auth config: %w", toAddress, err)
			}
			if authConfig.MaxValue != "" {
				if _, err := hexutil.DecodeBig(authConfig.MaxValue); err != nil {
					return config, fmt.Errorf("invalid maxValue '%s' in auth config: %w", toAddress, err)
				}
			}
		}
	}
	for _, proxyConfig := range config.Proxy {
		u, err := url.Parse(proxyConfig.ProxyEndpoint)
		if err != nil {
			return config, fmt.Errorf("invalid proxyEndpoint '%s': %w", proxyConfig.ProxyEndpoint, err)
		}
		if u.Scheme != "ws" && u.Scheme != "wss" {
			return config, fmt.Errorf("invalid proxyEndpoint '%s': must have ws/wss scheme", proxyConfig.ProxyEndpoint)
		}
	}
	return config, err
}

func (s SignerServiceConfig) GetAuthConfigForClient(clientName string, fromAddress *common.Address) (*AuthConfig, error) {
	if clientName == "" {
		return nil, errors.New("client name is empty")
	}
	for _, ac := range s.Auth {
		if ac.ClientName == clientName {
			// If fromAddress is specified, it must match the address in the authConfig
			if fromAddress != nil && *fromAddress != ac.FromAddress {
				continue
			}

			return &ac, nil
		}
	}
	return nil, fmt.Errorf("client '%s' is not authorized to use any keys", clientName)
}
