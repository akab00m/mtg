package stats

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/9seconds/mtg/v2/events"
	"github.com/9seconds/mtg/v2/mtglib"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type prometheusProcessor struct {
	streams map[string]*streamInfo
	factory *PrometheusFactory
}

func (p prometheusProcessor) EventStart(evt mtglib.EventStart) {
	info := acquireStreamInfo()
	info.startTime = time.Now()

	if evt.RemoteIP.To4() != nil {
		info.tags[TagIPFamily] = TagIPFamilyIPv4
	} else {
		info.tags[TagIPFamily] = TagIPFamilyIPv6
	}

	p.streams[evt.StreamID()] = info

	p.factory.metricClientConnections.
		WithLabelValues(info.tags[TagIPFamily]).
		Inc()
}

func (p prometheusProcessor) EventConnectedToDC(evt mtglib.EventConnectedToDC) {
	info, ok := p.streams[evt.StreamID()]
	if !ok {
		return
	}

	info.tags[TagTelegramIP] = evt.RemoteIP.String()
	info.tags[TagDC] = strconv.Itoa(evt.DC)

	p.factory.metricTelegramConnections.
		WithLabelValues(info.tags[TagTelegramIP], info.tags[TagDC]).
		Inc()
}

func (p prometheusProcessor) EventDomainFronting(evt mtglib.EventDomainFronting) {
	info, ok := p.streams[evt.StreamID()]
	if !ok {
		return
	}

	info.isDomainFronted = true

	p.factory.metricDomainFronting.Inc()
	p.factory.metricDomainFrontingConnections.
		WithLabelValues(info.tags[TagIPFamily]).
		Inc()
}

func (p prometheusProcessor) EventTraffic(evt mtglib.EventTraffic) {
	info, ok := p.streams[evt.StreamID()]
	if !ok {
		return
	}

	direction := getDirection(evt.IsRead)

	// Записываем время первого байта для TTFB метрики
	if !info.hasFirstByte && evt.IsRead && evt.Traffic > 0 {
		info.firstByteTime = time.Now()
		info.hasFirstByte = true
		ttfb := info.firstByteTime.Sub(info.startTime).Seconds()
		p.factory.metricTTFB.Observe(ttfb)
	}

	if info.isDomainFronted {
		p.factory.metricDomainFrontingTraffic.
			WithLabelValues(direction).
			Add(float64(evt.Traffic))
	} else {
		p.factory.metricTelegramTraffic.
			WithLabelValues(info.tags[TagTelegramIP], info.tags[TagDC], direction).
			Add(float64(evt.Traffic))
	}
}

func (p prometheusProcessor) EventFinish(evt mtglib.EventFinish) {
	info, ok := p.streams[evt.StreamID()]
	if !ok {
		return
	}

	defer func() {
		delete(p.streams, evt.StreamID())
		releaseStreamInfo(info)
	}()

	// Записываем duration сессии для анализа throughput
	if !info.startTime.IsZero() {
		duration := time.Since(info.startTime).Seconds()
		p.factory.metricSessionDuration.Observe(duration)
	}

	p.factory.metricClientConnections.
		WithLabelValues(info.tags[TagIPFamily]).
		Dec()

	if info.isDomainFronted {
		p.factory.metricDomainFrontingConnections.
			WithLabelValues(info.tags[TagIPFamily]).
			Dec()
	} else if telegramIP, ok := info.tags[TagTelegramIP]; ok {
		p.factory.metricTelegramConnections.
			WithLabelValues(telegramIP, info.tags[TagDC]).
			Dec()
	}
}

func (p prometheusProcessor) EventConcurrencyLimited(_ mtglib.EventConcurrencyLimited) {
	p.factory.metricConcurrencyLimited.Inc()
}

func (p prometheusProcessor) EventIPBlocklisted(evt mtglib.EventIPBlocklisted) {
	tag := TagIPListBlock
	if !evt.IsBlockList {
		tag = TagIPListAllow
	}

	p.factory.metricIPBlocklisted.WithLabelValues(tag).Inc()
}

func (p prometheusProcessor) EventReplayAttack(_ mtglib.EventReplayAttack) {
	p.factory.metricReplayAttacks.Inc()
}

func (p prometheusProcessor) EventIPListSize(evt mtglib.EventIPListSize) {
	tag := TagIPListBlock
	if !evt.IsBlockList {
		tag = TagIPListAllow
	}

	p.factory.metricIPListSize.WithLabelValues(tag).Set(float64(evt.Size))
}

func (p prometheusProcessor) EventDNSCacheMetrics(evt mtglib.EventDNSCacheMetrics) {
	p.factory.UpdateDNSCacheMetrics(evt.DeltaHits, evt.DeltaMisses, evt.DeltaEvictions, evt.Size)
}

func (p prometheusProcessor) EventPoolMetrics(evt mtglib.EventPoolMetrics) {
	p.factory.UpdatePoolMetricsDelta(evt.DC, evt.DeltaHits, evt.DeltaMisses, evt.DeltaUnhealthy, evt.Idle)
}

func (p prometheusProcessor) Shutdown() {
	for k, v := range p.streams {
		releaseStreamInfo(v)
		delete(p.streams, k)
	}
}

// PrometheusFactory is a factory of [events.Observer] which collect
// information in a format suitable for Prometheus.
//
// This factory can also serve on a given listener. In that case it starts HTTP
// server with a single endpoint - a Prometheus-compatible scrape output.
type PrometheusFactory struct {
	httpServer *http.Server

	metricClientConnections         *prometheus.GaugeVec
	metricTelegramConnections       *prometheus.GaugeVec
	metricDomainFrontingConnections *prometheus.GaugeVec
	metricIPListSize                *prometheus.GaugeVec

	metricTelegramTraffic       *prometheus.CounterVec
	metricDomainFrontingTraffic *prometheus.CounterVec
	metricIPBlocklisted         *prometheus.CounterVec

	metricDomainFronting     prometheus.Counter
	metricConcurrencyLimited prometheus.Counter
	metricReplayAttacks      prometheus.Counter

	// Performance metrics (PHASE 3)
	metricDNSCacheHits      prometheus.Counter
	metricDNSCacheMisses    prometheus.Counter
	metricDNSCacheSize      prometheus.Gauge
	metricDNSCacheEvictions prometheus.Counter
	metricRateLimitRejects  prometheus.Counter

	// Mobile optimization metrics (PHASE 4)
	metricSessionDuration prometheus.Histogram // Длительность сессий для расчёта throughput
	metricTTFB            prometheus.Histogram // Time To First Byte для latency анализа

	// Connection pool metrics (PHASE 3.3)
	metricPoolHits      *prometheus.CounterVec // Успешные взятия из пула
	metricPoolMisses    *prometheus.CounterVec // Промахи (создание нового)
	metricPoolUnhealthy *prometheus.CounterVec // Отклонено нездоровых
	metricPoolIdle      *prometheus.GaugeVec   // Текущее количество idle

	// Build info metric
	metricBuildInfo *prometheus.GaugeVec
}

// Make builds a new observer.
func (p *PrometheusFactory) Make() events.Observer {
	return prometheusProcessor{
		streams: make(map[string]*streamInfo),
		factory: p,
	}
}

// Serve starts an HTTP server on a given listener.
func (p *PrometheusFactory) Serve(listener net.Listener) error {
	return p.httpServer.Serve(listener) //nolint: wrapcheck
}

// Close stops a factory. Please pay attention that underlying listener
// is not closed.
func (p *PrometheusFactory) Close() error {
	return p.httpServer.Shutdown(context.Background()) //nolint: wrapcheck
}

// UpdateDNSCacheMetrics updates DNS cache metrics from provided stats.
// This should be called periodically (e.g., every 10 seconds) to keep metrics fresh.
func (p *PrometheusFactory) UpdateDNSCacheMetrics(hits, misses, evictions uint64, size int) {
	p.metricDNSCacheHits.Add(float64(hits))
	p.metricDNSCacheMisses.Add(float64(misses))
	p.metricDNSCacheEvictions.Add(float64(evictions))
	p.metricDNSCacheSize.Set(float64(size))
}

// IncrementRateLimitRejects increments the rate limit rejection counter.
func (p *PrometheusFactory) IncrementRateLimitRejects() {
	p.metricRateLimitRejects.Inc()
}

// UpdatePoolMetrics updates connection pool metrics for a specific DC.
// Should be called periodically to keep metrics fresh.
func (p *PrometheusFactory) UpdatePoolMetrics(dc int, hits, misses, unhealthy uint64, idle int) {
	dcStr := strconv.Itoa(dc)
	// Reset and set counters based on total values
	// Note: Prometheus counters are monotonic, so we use Add with delta
	p.metricPoolHits.WithLabelValues(dcStr).Add(0)    // Just touch to create label
	p.metricPoolMisses.WithLabelValues(dcStr).Add(0)  // Just touch to create label
	p.metricPoolUnhealthy.WithLabelValues(dcStr).Add(0)
	p.metricPoolIdle.WithLabelValues(dcStr).Set(float64(idle))
}

// UpdatePoolMetricsDelta updates connection pool metrics with delta values.
func (p *PrometheusFactory) UpdatePoolMetricsDelta(dc int, deltaHits, deltaMisses, deltaUnhealthy uint64, idle int) {
	dcStr := strconv.Itoa(dc)
	if deltaHits > 0 {
		p.metricPoolHits.WithLabelValues(dcStr).Add(float64(deltaHits))
	}
	if deltaMisses > 0 {
		p.metricPoolMisses.WithLabelValues(dcStr).Add(float64(deltaMisses))
	}
	if deltaUnhealthy > 0 {
		p.metricPoolUnhealthy.WithLabelValues(dcStr).Add(float64(deltaUnhealthy))
	}
	p.metricPoolIdle.WithLabelValues(dcStr).Set(float64(idle))
}

// NewPrometheus builds an events.ObserverFactory which can serve HTTP
// endpoint with Prometheus scrape data.
func NewPrometheus(metricPrefix, httpPath, version string) *PrometheusFactory { //nolint: funlen
	registry := prometheus.NewPedanticRegistry()
	httpHandler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
	mux := http.NewServeMux()

	mux.Handle(httpPath, httpHandler)

	factory := &PrometheusFactory{
		httpServer: &http.Server{
			Handler: mux,
		},

		metricClientConnections: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricPrefix,
			Name:      MetricClientConnections,
			Help:      "A number of actively processing client connections.",
		}, []string{TagIPFamily}),
		metricTelegramConnections: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricPrefix,
			Name:      MetricTelegramConnections,
			Help:      "A number of connections to Telegram servers.",
		}, []string{TagTelegramIP, TagDC}),
		metricDomainFrontingConnections: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricPrefix,
			Name:      MetricDomainFrontingConnections,
			Help:      "A number of connections which talk to front domain.",
		}, []string{TagIPFamily}),
		metricIPListSize: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricPrefix,
			Name:      MetricIPListSize,
			Help:      "A size of the ip list (blocklist or allowlist)",
		}, []string{TagIPList}),

		metricTelegramTraffic: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricPrefix,
			Name:      MetricTelegramTraffic,
			Help:      "Traffic which is generated talking with Telegram servers.",
		}, []string{TagTelegramIP, TagDC, TagDirection}),
		metricDomainFrontingTraffic: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricPrefix,
			Name:      MetricDomainFrontingTraffic,
			Help:      "Traffic which is generated talking with front domain.",
		}, []string{TagDirection}),
		metricIPBlocklisted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricPrefix,
			Name:      MetricIPBlocklisted,
			Help:      "A number of rejected sessions due to ip blocklisting.",
		}, []string{TagIPList}),

		metricDomainFronting: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricPrefix,
			Name:      MetricDomainFronting,
			Help:      "A number of routings to front domain.",
		}),
		metricConcurrencyLimited: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricPrefix,
			Name:      MetricConcurrencyLimited,
			Help:      "A number of sessions that were rejected by concurrency limiter.",
		}),
		metricReplayAttacks: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricPrefix,
			Name:      MetricReplayAttacks,
			Help:      "A number of detected replay attacks.",
		}),

		// Performance metrics (PHASE 3)
		metricDNSCacheHits: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricPrefix,
			Name:      "dns_cache_hits",
			Help:      "Number of DNS cache hits (successful lookups from cache).",
		}),
		metricDNSCacheMisses: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricPrefix,
			Name:      "dns_cache_misses",
			Help:      "Number of DNS cache misses (queries that required DNS resolution).",
		}),
		metricDNSCacheSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricPrefix,
			Name:      "dns_cache_size",
			Help:      "Current number of entries in DNS cache.",
		}),
		metricDNSCacheEvictions: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricPrefix,
			Name:      "dns_cache_evictions",
			Help:      "Number of DNS cache entries evicted due to LRU policy.",
		}),
		metricRateLimitRejects: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricPrefix,
			Name:      "rate_limit_rejects",
			Help:      "Number of connections rejected due to rate limiting.",
		}),

		// Mobile optimization metrics (PHASE 4)
		metricSessionDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: metricPrefix,
			Name:      "session_duration_seconds",
			Help:      "Duration of client sessions in seconds. Use with traffic metrics to calculate throughput.",
			Buckets:   []float64{0.1, 0.5, 1, 5, 10, 30, 60, 120, 300, 600},
		}),
		metricTTFB: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: metricPrefix,
			Name:      "time_to_first_byte_seconds",
			Help:      "Time from connection start to first byte received (download latency indicator).",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
		}),

		// Connection pool metrics (PHASE 3.3)
		metricPoolHits: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricPrefix,
			Name:      "connection_pool_hits_total",
			Help:      "Number of connections successfully retrieved from pool.",
		}, []string{TagDC}),
		metricPoolMisses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricPrefix,
			Name:      "connection_pool_misses_total",
			Help:      "Number of pool misses (new connections created).",
		}, []string{TagDC}),
		metricPoolUnhealthy: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricPrefix,
			Name:      "connection_pool_unhealthy_total",
			Help:      "Number of unhealthy connections rejected from pool.",
		}, []string{TagDC}),
		metricPoolIdle: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricPrefix,
			Name:      "connection_pool_idle",
			Help:      "Current number of idle connections in pool.",
		}, []string{TagDC}),

		// Build info metric
		metricBuildInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricPrefix,
			Name:      "build_info",
			Help:      "Build information about mtg proxy.",
		}, []string{"version"}),
	}

	registry.MustRegister(factory.metricClientConnections)
	registry.MustRegister(factory.metricTelegramConnections)
	registry.MustRegister(factory.metricDomainFrontingConnections)
	registry.MustRegister(factory.metricIPListSize)

	registry.MustRegister(factory.metricTelegramTraffic)
	registry.MustRegister(factory.metricDomainFrontingTraffic)
	registry.MustRegister(factory.metricIPBlocklisted)

	registry.MustRegister(factory.metricDomainFronting)
	registry.MustRegister(factory.metricConcurrencyLimited)
	registry.MustRegister(factory.metricReplayAttacks)

	// Register performance metrics (PHASE 3)
	registry.MustRegister(factory.metricDNSCacheHits)
	registry.MustRegister(factory.metricDNSCacheMisses)
	registry.MustRegister(factory.metricDNSCacheSize)
	registry.MustRegister(factory.metricDNSCacheEvictions)
	registry.MustRegister(factory.metricRateLimitRejects)

	// Register mobile optimization metrics (PHASE 4)
	registry.MustRegister(factory.metricSessionDuration)
	registry.MustRegister(factory.metricTTFB)

	// Register connection pool metrics (PHASE 3.3)
	registry.MustRegister(factory.metricPoolHits)
	registry.MustRegister(factory.metricPoolMisses)
	registry.MustRegister(factory.metricPoolUnhealthy)
	registry.MustRegister(factory.metricPoolIdle)

	// Register build info metric and set version
	registry.MustRegister(factory.metricBuildInfo)
	factory.metricBuildInfo.WithLabelValues(version).Set(1)

	return factory
}
