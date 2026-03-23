package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// PrometheusMetrics holds all Prometheus metric registrations for the AI Corp platform.
// These metrics follow Prometheus naming conventions and can be scraped by Prometheus server.
var (
	// === Inference Metrics ===

	InferenceRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "aicorp",
		Subsystem: "inference",
		Name:      "requests_total",
		Help:      "Total number of LLM inference requests",
	}, []string{"model", "provider", "status"})

	InferenceLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "aicorp",
		Subsystem: "inference",
		Name:      "latency_seconds",
		Help:      "Inference request latency in seconds",
		Buckets:   []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
	}, []string{"model", "provider"})

	InferenceTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "aicorp",
		Subsystem: "inference",
		Name:      "tokens_total",
		Help:      "Total tokens processed",
	}, []string{"model", "direction"}) // direction: prompt / completion

	InferenceTTFT = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "aicorp",
		Subsystem: "inference",
		Name:      "ttft_seconds",
		Help:      "Time To First Token in seconds",
		Buckets:   []float64{0.05, 0.1, 0.2, 0.5, 1, 2, 5},
	}, []string{"model"})

	InferenceTPS = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "inference",
		Name:      "tokens_per_second",
		Help:      "Current tokens per second throughput",
	}, []string{"model"})

	InferenceCacheHits = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "aicorp",
		Subsystem: "inference",
		Name:      "cache_hits_total",
		Help:      "Total KV cache hits",
	}, []string{"model"})

	InferenceCacheMisses = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "aicorp",
		Subsystem: "inference",
		Name:      "cache_misses_total",
		Help:      "Total KV cache misses",
	}, []string{"model"})

	// === Agent Metrics ===

	AgentTasksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "aicorp",
		Subsystem: "agent",
		Name:      "tasks_total",
		Help:      "Total tasks processed by agents",
	}, []string{"agent_role", "status"}) // status: success / failed

	AgentTaskLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "aicorp",
		Subsystem: "agent",
		Name:      "task_latency_seconds",
		Help:      "Agent task processing latency",
		Buckets:   []float64{1, 5, 10, 30, 60, 120, 300},
	}, []string{"agent_role", "task_type"})

	AgentActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "agent",
		Name:      "active",
		Help:      "Number of active agents by role",
	}, []string{"role"})

	// === Workflow Metrics ===

	WorkflowRunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "aicorp",
		Subsystem: "workflow",
		Name:      "runs_total",
		Help:      "Total workflow executions",
	}, []string{"workflow_name", "status"})

	WorkflowDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "aicorp",
		Subsystem: "workflow",
		Name:      "duration_seconds",
		Help:      "Workflow execution duration",
		Buckets:   []float64{10, 30, 60, 120, 300, 600, 1800},
	}, []string{"workflow_name"})

	// === Database Metrics ===

	DBQueryLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "aicorp",
		Subsystem: "db",
		Name:      "query_latency_seconds",
		Help:      "Database query latency",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
	}, []string{"operation"}) // operation: select / insert / update

	DBConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "db",
		Name:      "connections_active",
		Help:      "Number of active database connections",
	})

	DBConnectionsIdle = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "db",
		Name:      "connections_idle",
		Help:      "Number of idle database connections",
	})

	// === HTTP API Metrics ===

	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "aicorp",
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "Total HTTP requests",
	}, []string{"method", "path", "status"})

	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "aicorp",
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "HTTP request duration",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path"})

	// === System Metrics ===

	SystemGoroutines = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "system",
		Name:      "goroutines",
		Help:      "Number of active goroutines",
	})

	SystemMemoryBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "system",
		Name:      "memory_alloc_bytes",
		Help:      "Current memory allocation in bytes",
	})

	// === Knowledge Base Metrics ===

	KBSearchLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "aicorp",
		Subsystem: "kb",
		Name:      "search_latency_seconds",
		Help:      "Knowledge base search latency",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1},
	}, []string{"search_type"}) // search_type: vector / text

	KBEntriesTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "kb",
		Name:      "entries_total",
		Help:      "Total knowledge base entries",
	})
)

// RecordInference is a convenience function to record all inference-related metrics at once.
func RecordInference(model, provider, status string, latencySec, ttftSec, tps float64, promptTokens, completionTokens int, cacheHit bool) {
	InferenceRequestsTotal.WithLabelValues(model, provider, status).Inc()
	InferenceLatency.WithLabelValues(model, provider).Observe(latencySec)
	InferenceTokensTotal.WithLabelValues(model, "prompt").Add(float64(promptTokens))
	InferenceTokensTotal.WithLabelValues(model, "completion").Add(float64(completionTokens))

	if ttftSec > 0 {
		InferenceTTFT.WithLabelValues(model).Observe(ttftSec)
	}
	if tps > 0 {
		InferenceTPS.WithLabelValues(model).Set(tps)
	}

	if cacheHit {
		InferenceCacheHits.WithLabelValues(model).Inc()
	} else {
		InferenceCacheMisses.WithLabelValues(model).Inc()
	}
}
