package metrics

import (
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// System-level metrics using gopsutil

var (
	// CPU usage percentage (0-100)
	SystemCPUUsage = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "system",
		Name:      "cpu_usage_percent",
		Help:      "System CPU usage percentage",
	})

	// Memory usage
	SystemMemoryUsed = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "system",
		Name:      "memory_used_bytes",
		Help:      "System memory used in bytes",
	})

	SystemMemoryTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "system",
		Name:      "memory_total_bytes",
		Help:      "System total memory in bytes",
	})

	SystemMemoryUsagePercent = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "system",
		Name:      "memory_usage_percent",
		Help:      "System memory usage percentage",
	})

	// Network I/O
	SystemNetworkBytesIn = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "system",
		Name:      "network_bytes_in_total",
		Help:      "Total network bytes received",
	})

	SystemNetworkBytesOut = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "system",
		Name:      "network_bytes_out_total",
		Help:      "Total network bytes sent",
	})

	// Go runtime metrics (already defined but update here for completeness)
	SystemGoMemoryAlloc = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "system",
		Name:      "go_memory_alloc_bytes",
		Help:      "Go runtime memory allocation in bytes",
	})

	SystemGoGoroutines = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "system",
		Name:      "go_goroutines",
		Help:      "Number of active goroutines",
	})
)

// SystemCollector collects system-level metrics periodically.
type SystemCollector struct {
	lastNetIO uint64
	lastTime  time.Time
}

// NewSystemCollector creates a new system metrics collector.
func NewSystemCollector() *SystemCollector {
	return &SystemCollector{
		lastTime: time.Now(),
	}
}

// Collect gathers system metrics. Call this periodically.
func (sc *SystemCollector) Collect() {
	// CPU usage (1 second interval for accurate reading)
	cpuPct, err := cpu.Percent(1*time.Second, false)
	if err == nil && len(cpuPct) > 0 {
		SystemCPUUsage.Set(cpuPct[0])
	}

	// Memory usage
	if vmStat, err := mem.VirtualMemory(); err == nil {
		SystemMemoryUsed.Set(float64(vmStat.Used))
		SystemMemoryTotal.Set(float64(vmStat.Total))
		SystemMemoryUsagePercent.Set(vmStat.UsedPercent)
	}

	// Network I/O (cumulative counters)
	if netIO, err := net.IOCounters(false); err == nil && len(netIO) > 0 {
		SystemNetworkBytesIn.Set(float64(netIO[0].BytesRecv))
		SystemNetworkBytesOut.Set(float64(netIO[0].BytesSent))
	}

	// Go runtime metrics
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	SystemGoMemoryAlloc.Set(float64(m.Alloc))
	SystemGoGoroutines.Set(float64(runtime.NumGoroutine()))
}

// StartPeriodicCollection starts a goroutine that collects metrics at the given interval.
func (sc *SystemCollector) StartPeriodicCollection(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			sc.Collect()
		}
	}()
}
