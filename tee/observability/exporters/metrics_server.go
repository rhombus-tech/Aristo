// Package exporters provides HTTP servers to expose metrics
package exporters

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rhombus-tech/vm/tee/observability/metrics"
)

// MetricsServer exposes TEE metrics via HTTP
type MetricsServer struct {
	server   *http.Server
	registry *prometheus.Registry
	metrics  *metrics.TEEMetrics
	
	// Server configuration
	port     int
	teeID    string
	regionID string
}

// NewMetricsServer creates a new metrics server
func NewMetricsServer(port int, teeID, regionID string) *MetricsServer {
	registry := prometheus.NewRegistry()
	
	server := &MetricsServer{
		registry: registry,
		metrics:  metrics.NewTEEMetrics(registry),
		port:     port,
		teeID:    teeID,
		regionID: regionID,
	}
	
	return server
}

// Start starts the metrics server
func (s *MetricsServer) Start() error {
	mux := http.NewServeMux()
	
	// Register the metrics handler
	mux.Handle("/metrics", promhttp.HandlerFor(
		s.registry,
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
			Registry:          s.registry,
		},
	))
	
	// Add a simple status endpoint
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("TEE Metrics Server\nRegion: %s\nTEE ID: %s\nActive: true", 
			s.regionID, s.teeID)))
	})
	
	// Create the HTTP server
	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}
	
	// Start the server in a goroutine
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Error starting metrics server: %v\n", err)
		}
	}()
	
	fmt.Printf("Metrics server started on port %d\n", s.port)
	return nil
}

// Stop gracefully shuts down the metrics server
func (s *MetricsServer) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// GetMetrics returns the TEE metrics collector
func (s *MetricsServer) GetMetrics() *metrics.TEEMetrics {
	return s.metrics
}
