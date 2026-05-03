package metrics

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	registerOnce sync.Once

	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of processed HTTP requests.",
		},
		[]string{"method", "route", "status"},
	)

	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.3, 1, 3},
		},
		[]string{"method", "route"},
	)

	httpInFlightRequests = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "http_in_flight_requests",
			Help: "Current number of in-flight HTTP requests.",
		},
	)
)

func MustRegister() {
	registerOnce.Do(func() {
		prometheus.MustRegister(httpRequestsTotal, httpRequestDuration, httpInFlightRequests)
	})
}

func Handler() http.Handler {
	MustRegister()
	return promhttp.Handler()
}

type statusCapturingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *statusCapturingResponseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func InstrumentHTTP(next http.Handler) http.Handler {
	MustRegister()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpInFlightRequests.Inc()
		defer httpInFlightRequests.Dec()

		start := time.Now()
		rw := &statusCapturingResponseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(rw, r)

		route := routeLabel(r)
		status := strconv.Itoa(rw.statusCode)
		duration := time.Since(start).Seconds()

		httpRequestsTotal.WithLabelValues(r.Method, route, status).Inc()
		httpRequestDuration.WithLabelValues(r.Method, route).Observe(duration)
	})
}

func routeLabel(r *http.Request) string {
	if r.Pattern != "" {
		return normalizeRoutePattern(r.Pattern)
	}
	return normalizeRoutePattern(r.URL.Path)
}

func normalizeRoutePattern(pattern string) string {
	switch pattern {
	case "GET /v1/tasks/{id}", "PATCH /v1/tasks/{id}", "DELETE /v1/tasks/{id}":
		return "/v1/tasks/:id"
	case "GET /v1/tasks", "POST /v1/tasks":
		return "/v1/tasks"
	case "GET /metrics":
		return "/metrics"
	default:
		return pattern
	}
}
