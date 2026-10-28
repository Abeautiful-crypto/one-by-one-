// 工单08：Go 后端性能分析与调优工具链集成
// 集成 net/http/pprof、expvar、runtime/metrics、Prometheus 指标导出，并预留 gops/pyroscope 接入点。
// 验收：分析接口受 JWT 鉴权保护（防敏感数据泄露）；采样对业务影响极低；配置化程度高。
package perf

import (
	"expvar"
	"net/http"
	"net/http/pprof"
	"runtime"
	"runtime/metrics"
	"strconv"
	"time"

	"ride-hailing/internal/config"
	"ride-hailing/internal/middleware"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ---- Prometheus 指标（工单08/09 共享：接口时延、QPS、运行时） ----
var (
	ReqTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "ride",
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "HTTP 请求总数",
	}, []string{"method", "path", "code"})

	ReqDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "ride",
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "HTTP 请求耗时分布",
		Buckets:   []float64{0.005, 0.01, 0.05, 0.1, 0.3, 0.5, 1, 3, 5},
	}, []string{"method", "path"})

	Goroutines = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "ride", Subsystem: "runtime", Name: "goroutines", Help: "当前 goroutine 数",
	})
	HeapInuse = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "ride", Subsystem: "runtime", Name: "heap_inuse_bytes", Help: "堆内存使用字节",
	})
	GCCount = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "ride", Subsystem: "runtime", Name: "gc_total", Help: "GC 次数",
	})
)

// Init 注册 Prometheus 采集器并启动 runtime/metrics -> expvar 周期采集（工单08）
func Init() {
	prometheus.MustRegister(ReqTotal, ReqDuration, Goroutines, HeapInuse, GCCount)
	startRuntimeCollector()
}

// RequestMetrics Gin 中间件：采集每个请求的 QPS 与耗时（工单08 运行时监控）
func RequestMetrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		dur := time.Since(start).Seconds()
		ReqTotal.WithLabelValues(c.Request.Method, c.FullPath(), strconv.Itoa(c.Writer.Status())).Inc()
		ReqDuration.WithLabelValues(c.Request.Method, c.FullPath()).Observe(dur)
	}
}

// MetricsHandler Prometheus 拉取端点 /metrics（工单08 暴露运行时指标）
func MetricsHandler() gin.HandlerFunc {
	h := promhttp.Handler()
	return func(c *gin.Context) { h.ServeHTTP(c.Writer, c.Request) }
}

// RegisterPprof 注册 pprof 路由到 /debug/pprof（工单08：分析接口需限制访问权限，防敏感数据泄露）
// 所有分析端点必须经 JWT 鉴权 + 管理员角色，确保非授权不可访问 CPU/Heap/Mem/Block/Goroutine Profile。
func RegisterPprof(r *gin.Engine) {
	g := r.Group("/debug/pprof")
	g.Use(middleware.JWTAuth(config.Load()), middleware.RequireAdmin()) // 仅管理员可访问性能分析接口
	{
		g.GET("/", gin.WrapF(pprof.Index))
		g.GET("/cmdline", gin.WrapF(pprof.Cmdline))
		g.GET("/profile", gin.WrapF(pprof.Profile))   // CPU Profile
		g.GET("/symbol", gin.WrapF(pprof.Symbol))
		g.GET("/trace", gin.WrapF(pprof.Trace))       // 执行追踪
		// Heap / Block / Goroutine / ThreadCreate / Alloc / Mutex 等 Profile
		g.GET("/heap", gin.WrapF(pprof.Index))
		g.GET("/block", gin.WrapF(pprof.Index))
		g.GET("/goroutine", gin.WrapF(pprof.Index))
		g.GET("/allocs", gin.WrapF(pprof.Index))
		g.GET("/mutex", gin.WrapF(pprof.Index))
		g.GET("/threadcreate", gin.WrapF(pprof.Index))
	}
}

// ---- runtime/metrics 周期采集到 expvar（工单08：GC 次数、内存、goroutine、分配速率） ----
var (
	expGoroutines = expvar.NewInt("runtime_goroutines")
	expHeap      = expvar.NewInt("runtime_heap_inuse_bytes")
	expGcCount   = expvar.NewInt("runtime_gc_total")
	expGcPause   = expvar.NewFloat("runtime_gc_pause_ms")
	expAllocRate = expvar.NewFloat("runtime_alloc_bytes_per_sec")
)

func startRuntimeCollector() {
	samples := []metrics.Sample{{Name: "/gc/cycles:gc-cycles:total"}}
	var lastGc float64
	var lastHeap uint64
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)
			expGoroutines.Set(int64(runtime.NumGoroutine()))
			expHeap.Set(int64(memStats.HeapInuse))

			metrics.Read(samples)
			if samples[0].Value.Kind() == metrics.KindFloat64 {
				cur := samples[0].Value.Float64()
				if lastGc > 0 {
					GCCount.Add(cur - lastGc)
					expGcCount.Add(int64(cur - lastGc))
				}
				lastGc = cur
			}
			if lastHeap > 0 {
				allocDelta := int64(memStats.HeapAlloc) - int64(lastHeap)
				expAllocRate.Set(float64(allocDelta) / 5.0)
			}
			lastHeap = memStats.HeapAlloc
			expGcPause.Set(float64(memStats.PauseNs[(memStats.NumGC+255)%256]) / 1e6)

			// 同步到 Prometheus
			Goroutines.Set(float64(runtime.NumGoroutine()))
			HeapInuse.Set(float64(memStats.HeapInuse))
		}
	}()
}

// GopsListen 启动 gops agent（工单08：实时进程状态查询）。仅当 GOPS_ADDR 设置时启用。
func GopsListen(addr string) {
	if addr == "" {
		return
	}
	// gops agent 在 main 中通过 import 包启动，这里仅暴露配置入口
	_ = addr
}

var _ = http.StatusOK
