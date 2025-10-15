package monitor

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
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
	"github.com/ethereum/go-ethereum/crypto"
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

const (
	rpcMaxAttempts       = 2
	rpcInitialBackoff    = 500 * time.Millisecond
	rpcRequestTimeout    = 3 * time.Second
	rpcBackoffMultiplier = 2
)

func (p *Poller) pollPing(ctx context.Context) (err error) {
	start := time.Now()
	parsedURL, err := url.Parse(p.config.SignerConfig.Address)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	host := parsedURL.Hostname()
	address := net.JoinHostPort(host, p.config.SignerConfig.Port)

	defer func() {
		latency := time.Since(start)

		log.Debug("finished ping", "latency", latency, "err", err)

		metrics.RecordPingSuccess(address, err == nil)
		metrics.RecordPingLatency(address, latency)
		if err != nil {
			metrics.RecordErrorDetails(address, err)
		}
	}()

	cert, err := tls.LoadX509KeyPair(p.config.SignerConfig.TLSCert, p.config.SignerConfig.TLSKey)
	if err != nil {
		err = fmt.Errorf("failed to load client certificate and key: %w", err)
		return
	}

	caCert, err := os.ReadFile(p.config.SignerConfig.TLSCaCert)
	if err != nil {
		err = fmt.Errorf("failed to read CA certificate file: %w", err)
		return
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		err = fmt.Errorf("failed to append CA certificate")
		return
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	}
	conn, err := tls.DialWithDialer(dialer, "tcp", address, tlsConfig)

	if err == nil {
		defer func() {
			err = conn.Close()
		}()
	}

	return
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
		err = fmt.Errorf("failed to load client certificate and key: %w", err)
		return
	}

	certExpiry, err := getCertExpiry(cert)
	if err == nil {
		metrics.RecordCertExpiry(endpoint, certExpiry)
	} else {
		log.Error("failed to get remaining time", "err", err)
	}

	caCert, err := os.ReadFile(p.config.SignerConfig.TLSCaCert)
	if err != nil {
		err = fmt.Errorf("failed to read CA certificate file: %w", err)
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
		Timeout: rpcRequestTimeout,
	}

	// Construct the JSON request body.
	payloadHash := crypto.Keccak256([]byte("dummy"))
	reqBody := RequestBody{
		JSONRPC: "2.0",
		Method:  "opsigner_signBlockPayload",
		Params: []BlockPayloadArgs{
			{
				Domain:        [32]byte{},
				ChainID:       p.config.RPCOptions.ChainID,
				SenderAddress: p.config.RPCOptions.FromAddress,
				PayloadHash:   payloadHash,
			},
		},
		ID: 1,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		err = fmt.Errorf("error marshaling JSON: %w", err)
		return
	}

	var (
		responseBody []byte
		statusCode   int
		rpcErr       error
		lastErr      error
	)

	backoff := rpcInitialBackoff

	for attempt := 1; attempt <= rpcMaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		default:
		}

		responseBody, statusCode, rpcErr = performRPCRequest(ctx, client, endpoint, jsonData)
		if rpcErr == nil && statusCode == http.StatusOK {
			break
		}

		shouldRetry := false
		if rpcErr != nil {
			shouldRetry = isRetryableRPCError(rpcErr)
			lastErr = fmt.Errorf("HTTP request failed: %w", rpcErr)
		} else {
			lastErr = fmt.Errorf("unexpected status code: %d\nResponse: %s", statusCode, responseBody)
			shouldRetry = isRetryableStatus(statusCode)
		}

		if !shouldRetry || attempt >= rpcMaxAttempts {
			err = lastErr
			return
		}

		log.Debug("retrying RPC request", "attempt", attempt, "err", lastErr)

		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		case <-time.After(backoff):
		}

		backoff *= rpcBackoffMultiplier
	}

	if lastErr != nil {
		err = lastErr
		return
	}

	body := responseBody

	var rpcResp RPCResponse
	err = json.Unmarshal(body, &rpcResp)
	if err != nil {
		err = fmt.Errorf("failed to unmarshal RPC response: %w", err)
		return
	}

	if rpcResp.Error != nil {
		err = fmt.Errorf("RPC error (code %d): %s", rpcResp.Error.Code, rpcResp.Error.Message)
		return
	}

	log.Debug("received RPC response", "body", body)
	return
}

func getCertExpiry(cert tls.Certificate) (time.Duration, error) {
	if len(cert.Certificate) == 0 {
		return 0, fmt.Errorf("empty cert")
	}
	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return 0, err
	}
	return time.Until(x509Cert.NotAfter), nil
}

func performRPCRequest(ctx context.Context, client *http.Client, endpoint string, jsonData []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonData))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return body, resp.StatusCode, nil
}

func isRetryableRPCError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}

	return false
}

func isRetryableStatus(status int) bool {
	if status == http.StatusTooManyRequests || status == http.StatusRequestTimeout {
		return true
	}

	return status >= http.StatusInternalServerError
}
