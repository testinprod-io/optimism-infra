package monitor

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/ethereum-optimism/optimism/op-signer-mon/pkg/metrics"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

type RequestBody struct {
	JSONRPC string             `json:"jsonrpc"`
	Method  string             `json:"method"`
	Params  []BlockPayloadArgs `json:"params"`
	ID      int                `json:"id"`
}

type BlockPayloadArgs struct {
	Domain        [32]byte        `json:"domain"`
	ChainID       *big.Int        `json:"chainId"`
	PayloadHash   []byte          `json:"payloadHash"`
	SenderAddress *common.Address `json:"senderAddress"`
}

// RPCResponse represents the general JSON-RPC response.
type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Error   *RPCError       `json:"error,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

// RPCError represents an error structure in a JSON-RPC response.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (p *Poller) pollPing(ctx context.Context) (err error) {
	start := time.Now()

	parsedURL, err := url.Parse(p.config.SignerConfig.Address)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	host := parsedURL.Hostname()
	address := net.JoinHostPort(host, p.config.SignerConfig.Port)

	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.Dial("tcp", address)
	if err == nil {
		defer conn.Close()
	}

	latency := time.Since(start)

	metrics.RecordPingSuccess(address, err == nil)
	metrics.RecordPingLatency(address, latency)
	if err != nil {
		metrics.RecordErrorDetails(address, err)
	}

	log.Debug("finished ping", "latency", latency, "err", err)

	return err
}

func (p *Poller) pollRPC(ctx context.Context) (err error) {
	endpoint := p.config.SignerConfig.Address + ":" + p.config.SignerConfig.Port

	start := time.Now()

	defer func() {
		latency := time.Since(start)

		metrics.RecordRPCSuccess(endpoint, err == nil)
		metrics.RecordRPCLatency(endpoint, latency)
		if err != nil {
			metrics.RecordErrorDetails(endpoint, err)
		}

		log.Debug("finished RPC", "latency", latency, "err", err)
	}()

	cert, err := tls.LoadX509KeyPair(p.config.SignerConfig.TLSCert, p.config.SignerConfig.TLSKey)
	if err != nil {
		err = fmt.Errorf("failed to load client certificate and key", "err", err)
		return
	}

	caCert, err := os.ReadFile(p.config.SignerConfig.TLSCaCert)
	if err != nil {
		err = fmt.Errorf("failed to read CA certificate file", "err", err)
		return
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		err = fmt.Errorf("failed to append CA certificate")
		return
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
				RootCAs:      caCertPool,
			},
		},
		Timeout: time.Second * 5,
	}

	// Construct the JSON request body.
	reqBody := RequestBody{
		JSONRPC: "2.0",
		Method:  "opsigner_signBlockPayload",
		Params: []BlockPayloadArgs{
			{
				Domain:        [32]byte{},
				ChainID:       p.config.RPCOptions.ChainID,
				PayloadHash:   []byte("c0ffee"),
				SenderAddress: p.config.RPCOptions.FromAddress,
			},
		},
		ID: 1,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		err = fmt.Errorf("error marshaling JSON", "err", err)
		return
	}
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(jsonData))
	if err != nil {
		err = fmt.Errorf("error creating HTTP request", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		err = fmt.Errorf("HTTP request failed", "err", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		err = fmt.Errorf("rror reading response", "err", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("unexpected status code: %d\nResponse: %s", resp.StatusCode, body)
		return
	}

	var rpcResp RPCResponse
	err = json.Unmarshal(body, &rpcResp)
	if err != nil {
		err = fmt.Errorf("failed to unmarshal RPC response", "err", err)
		return
	}

	if rpcResp.Error != nil {
		err = fmt.Errorf("RPC error (code %d): %s", rpcResp.Error.Code, rpcResp.Error.Message)
		return
	}

	log.Debug("received RPC response", "body", body)
	return
}
