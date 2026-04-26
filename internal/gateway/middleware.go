package gateway

import (
	"log"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"proxy2api/internal/config"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "proxy2api_http_requests_total",
		Help: "Total HTTP requests",
	}, []string{"method", "path", "status"})

	httpLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "proxy2api_http_request_duration_seconds",
		Help:    "HTTP request latency in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func withMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		status := strconv.Itoa(rec.status)
		httpRequestsTotal.WithLabelValues(r.Method, r.URL.Path, status).Inc()
		httpLatency.WithLabelValues(r.Method, r.URL.Path).Observe(time.Since(start).Seconds())
	})
}

func withAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("access method=%s path=%s status=%d dur=%s ua=%q", r.Method, r.URL.Path, rec.status, time.Since(start).String(), r.UserAgent())
	})
}

func withIPFilter(sec config.SecurityConfig, next http.Handler) http.Handler {
	allow := parseCIDRs(sec.IPAllowlist)
	deny := parseCIDRs(sec.IPDenylist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ipStr := clientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
		ip, err := netip.ParseAddr(ipStr)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		for _, p := range deny {
			if p.Contains(ip) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		if len(allow) > 0 {
			ok := false
			for _, p := range allow {
				if p.Contains(ip) {
					ok = true
					break
				}
			}
			if !ok {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func parseCIDRs(items []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(items))
	for _, raw := range items {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if strings.Contains(raw, "/") {
			if p, err := netip.ParsePrefix(raw); err == nil {
				out = append(out, p)
			}
			continue
		}
		if ip, err := netip.ParseAddr(raw); err == nil {
			bits := 32
			if ip.Is6() {
				bits = 128
			}
			out = append(out, netip.PrefixFrom(ip, bits))
		}
	}
	return out
}

func clientIP(remote, xff string) string {
	if xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			v := strings.TrimSpace(parts[0])
			if v != "" {
				return v
			}
		}
	}
	host := remote
	if i := strings.LastIndex(remote, ":"); i > 0 {
		host = remote[:i]
	}
	host = strings.Trim(host, "[]")
	return host
}
