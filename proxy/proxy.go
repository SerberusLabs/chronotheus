// Chronotheus - Time-traveling Prometheus Metrics Proxy
// Copyright (C) 2025 Andy Dixon <andy@andydixon.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package proxy

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"sync"
	"sync/atomic"
	"time"
)

// Configuration options for ChronoProxy
type Config struct {
	MaxIdleConns        int           // Maximum number of idle connections (like spare time machines)
	MaxIdleConnsPerHost int           // Max idle connections per destination (don't hog all the parking spots!)
	IdleConnTimeout     time.Duration // How long before we shut down an idle connection (power saving!)
	ClientTimeout       time.Duration // Maximum time for a complete operation (we can't wait forever!)
	DialTimeout         time.Duration // How long to wait for initial connection (patience has limits!)
	KeepAlive          time.Duration // Keep connections warm and ready (like keeping the engine running)
	DisableCompression  bool         // Whether to compress data (squish those bytes!)
	ForceAttemptHTTP2   bool         // Try to use HTTP/2 (the future is now!)
}

// Default configuration values
// These are like the factory settings - good for most people, but you can change them!
var DefaultConfig = Config{
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 100,
	IdleConnTimeout:     90 * time.Second,
	ClientTimeout:       30 * time.Second,
	DialTimeout:         5 * time.Second,
	KeepAlive:          30 * time.Second,
	DisableCompression:  false,
	ForceAttemptHTTP2:   true,
}

// Metrics for monitoring proxy performance
// These are our dashboard gauges - they tell us how well our time machine is running!
type ProxyMetrics struct {
	RequestCount      uint64    // Number of requests processed (our odometer!)
	ErrorCount        uint64    // Number of errors encountered (oops counter!)
	LastRequestTime   time.Time // When was our last adventure?
	AverageLatency   float64   // How long requests typically take (are we getting slower?)
	RequestsInFlight int64     // Current number of active requests (how busy are we?)
}

// ChronoProxy is our time-traveling traffic director! 
// Think of it like a magical switchboard operator who can:
// - Talk to Prometheus servers (client)
// - Remember different time windows (timeframes)
// - Know how far back to look (offsets)
//
// It's the brain behind all our time-window magic!
type ChronoProxy struct {
	offsets    []int64       // How many seconds to look back (0 = now, 604800 = 7 days, etc)
	timeframes []string      // Human-friendly names ("current", "7days", etc)
	client     *http.Client  // Our phone line to Prometheus
	config     Config        // Configuration options
	metrics    ProxyMetrics  // Runtime metrics
	metricsMux sync.RWMutex  // Protects metrics access
}

// NewChronoProxyWithConfig creates a new proxy with custom configuration
// It's like building a custom time machine to your exact specifications!
// Want more connections? Different timeouts? This is your friend!
func NewChronoProxyWithConfig(config Config) *ChronoProxy {
	return &ChronoProxy{
		offsets: []int64{
			0,
			7 * 24 * 3600,
			14 * 24 * 3600,
			21 * 24 * 3600,
			28 * 24 * 3600,
		},
		timeframes: []string{"current", "7days", "14days", "21days", "28days"},
		client: &http.Client{
			Timeout: config.ClientTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        config.MaxIdleConns,
				MaxIdleConnsPerHost: config.MaxIdleConnsPerHost,
				IdleConnTimeout:     config.IdleConnTimeout,
				DisableCompression:  config.DisableCompression,
				ForceAttemptHTTP2:   config.ForceAttemptHTTP2,
				DialContext: (&net.Dialer{
					Timeout:   config.DialTimeout,
					KeepAlive: config.KeepAlive,
				}).DialContext,
			},
		},
		config: config,
	}
}

// NewChronoProxy creates a new proxy with default configuration
func NewChronoProxy() *ChronoProxy {
	return NewChronoProxyWithConfig(DefaultConfig)
}

var (
	// Pre-compiled regex patterns
	// These are like our universal translators - they help us understand incoming requests!
	pathRegex     = regexp.MustCompile(`^/([^_/]+)_(\d+)(/.*)?$`)
	// Looking for label values? This pattern spots those requests!
	valuesRegex   = regexp.MustCompile(`^/api/v1/label/[^/]+/values$`)
	// Need to split a path? This is our path-chopping tool!
	pathSplitter  = regexp.MustCompile(`/`)
)

// ServeHTTP is Herr Traffik Direktor! 
// It looks at incoming requests and sends them to the right handler:
// - /api/v1/query:        Want a snapshot? This way! 
// - /api/v1/query_range:  Need a graph? Over here! 
// - /api/v1/labels:       Looking for label options? Follow me! 
// - /api/v1/label/.../values: Need specific values? Got you covered! 
// - anything else:        Just passing through! 
//
// Think of it like a helpful concierge who knows exactly where everything is!
// Each request gets the VIP treatment - routed to exactly where it needs to go.
//
// Pro tip: Watch the debug logs to see it in action - it's quite chatty!
func (p *ChronoProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var err error

	// Track requests in flight
	atomic.AddInt64(&p.metrics.RequestsInFlight, 1)
	defer atomic.AddInt64(&p.metrics.RequestsInFlight, -1)
	
	defer func() {
		p.updateMetrics(start, err)
	}()

	m := pathRegex.FindStringSubmatch(r.URL.Path)
	if m == nil {
		err = fmt.Errorf("invalid target prefix")
		http.Error(w, `{"status":"error","error":"Invalid target prefix"}`, http.StatusBadRequest)
		return
	}

	host, port, suffix := m[1], m[2], m[3]
	if suffix == "" {
		suffix = "/"
	}
	upstream := fmt.Sprintf("http://%s:%s", host, port)

	// Fast path for GET/POST methods
	if r.Method != "GET" && r.Method != "POST" {
		if DebugMode {
			log.Printf("Unsupported method %s, forwarding to upstream", r.Method)
		}
		forward(w, r, p.client, upstream+suffix)
		return
	}

	// Efficient routing using switch on suffix
	switch suffix {
	case "/api/v1/query":
		p.handleQuery(w, r, upstream, suffix)
		return
	case "/api/v1/query_range":
		p.handleQueryRange(w, r, upstream, suffix)
		return
	case "/api/v1/labels":
		p.handleLabels(w, r, upstream, suffix)
		return
	}

	// Check for label values endpoint
	if valuesRegex.MatchString(suffix) {
		parts := pathSplitter.Split(suffix, -1)
		if len(parts) >= 5 {
			p.handleLabelValues(w, r, upstream, suffix, parts[4])
			return
		}
	}

	if DebugMode {
		log.Printf("Forwarding Unknown request: %s %s\n", r.Method, r.URL.Path)
	}
	forward(w, r, p.client, upstream+suffix)
}

// GetMetrics returns current proxy metrics
// Want to know how your time machine is performing?
// This function is like checking the gauges on your dashboard!
func (p *ChronoProxy) GetMetrics() ProxyMetrics {
	p.metricsMux.RLock()
	defer p.metricsMux.RUnlock()
	return p.metrics
}

// updateMetrics updates proxy metrics for monitoring
// This is our flight recorder - keeping track of everything that happens!
// It helps us understand how well we're doing and where we can improve.
func (p *ChronoProxy) updateMetrics(start time.Time, err error) {
	p.metricsMux.Lock()
	defer p.metricsMux.Unlock()
	
	p.metrics.RequestCount++
	p.metrics.LastRequestTime = time.Now()
	
	if err != nil {
		p.metrics.ErrorCount++
	}
	
	latency := time.Since(start).Seconds()
	if p.metrics.RequestCount == 1 {
		p.metrics.AverageLatency = latency
	} else {
		// Exponential moving average with α=0.1
		p.metrics.AverageLatency = 0.1*latency + 0.9*p.metrics.AverageLatency
	}
}
