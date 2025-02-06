package metrics

import (
	"regexp"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	MetricsNamespace = "op_signer_mon"
)

var (
	Debug                bool
	nonAlphanumericRegex = regexp.MustCompile(`[^a-zA-Z ]+`)

	errorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: MetricsNamespace,
		Name:      "errors_total",
		Help:      "Count of errors",
	}, []string{
		"node",
		"error",
	})
	pingSuccess = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: MetricsNamespace,
		Name:      "ping_success",
		Help:      "Ping success (1 for success, 0 for failure)",
	}, []string{"target"})

	// pingLatency records the ping latency in milliseconds.
	pingLatency = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: MetricsNamespace,
		Name:      "ping_latency",
		Help:      "Ping latency in ms",
	}, []string{"target"})

	// rpcSuccess records whether an RPC call succeeded: 1 for success, 0 for failure.
	rpcSuccess = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: MetricsNamespace,
		Name:      "rpc_success",
		Help:      "RPC success (1 for success, 0 for failure)",
	}, []string{"node"})

	rpcLatency = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: MetricsNamespace,
		Name:      "rpc_latency",
		Help:      "RPC latency per network, node and method (ms)",
	}, []string{"node"})
)

func errLabel(err error) string {
	errClean := nonAlphanumericRegex.ReplaceAllString(err.Error(), "")
	errClean = strings.ReplaceAll(errClean, " ", "_")
	errClean = strings.ReplaceAll(errClean, "__", "_")
	return errClean
}

func RecordError(node, error string) {
	if Debug {
		log.Debug("metric inc",
			"m", "errors_total",
			"node", node,
			"error", error)
	}
	errorsTotal.WithLabelValues(node, error).Inc()
}

// RecordErrorDetails concats the error message to the label removing non-alpha chars
func RecordErrorDetails(label string, err error) {
	RecordError(label, errLabel(err))
}

func RecordRPCLatency(node string, latency time.Duration) {
	if Debug {
		log.Debug("metric set",
			"m", "rpc_latency",
			"node", node,
			"latency", latency)
	}
	rpcLatency.WithLabelValues(node).Set(float64(latency.Milliseconds()))
}
func RecordPingSuccess(target string, success bool) {
	if Debug {
		log.Debug("metric set",
			"m", "ping_success",
			"target", target,
			"success", success)
	}
	pingSuccess.WithLabelValues(target).Set(boolToFloat64(success))
}

// RecordPingLatency sets the ping_latency metric (in ms) for a given target.
func RecordPingLatency(target string, latency time.Duration) {
	if Debug {
		log.Debug("metric set",
			"m", "ping_latency",
			"target", target,
			"latency", latency.Milliseconds())
	}
	pingLatency.WithLabelValues(target).Set(float64(latency.Milliseconds()))
}

// RecordRPCSuccess sets the rpc_success metric (1 for success, 0 for failure) for a given node and method.
func RecordRPCSuccess(node string, success bool) {
	if Debug {
		log.Debug("metric set",
			"m", "rpc_success",
			"node", node,
			"success", success)
	}
	rpcSuccess.WithLabelValues(node).Set(boolToFloat64(success))
}

func boolToFloat64(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
