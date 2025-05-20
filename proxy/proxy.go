package proxy

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
)

var (
	// Pre-compiled regex patterns
	targetPrefixRegex = regexp.MustCompile(`^/([^_/]+)_(\d+)(/.*)?$`)
	labelValuesRegex  = regexp.MustCompile(`^/api/v1/label/[^/]+/values$`)
	pathSplitRegex    = regexp.MustCompile(`/`)
)

// ChronoProxy holds our offsets, labels and HTTP client.
type ChronoProxy struct {
	offsets    []int64
	timeframes []string
	client     *http.Client
}

// NewChronoProxy initializes the 0,7,14,21,28-day windows.
func NewChronoProxy() *ChronoProxy {
	return &ChronoProxy{
		offsets: []int64{
			0,
			7 * 24 * 3600,
			14 * 24 * 3600,
			21 * 24 * 3600,
			28 * 24 * 3600,
		},
		timeframes: []string{"current", "7days", "14days", "21days", "28days"},
		client:     &http.Client{},
	}
}

// ServeHTTP strips off /<host>_<port>/ and dispatches to handlers.
func (p *ChronoProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Add early method check
	if r.Method != "GET" && r.Method != "POST" {
		http.Error(w, `{"status":"error","error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Add early path length check
	if len(r.URL.Path) < 2 {
		http.Error(w, `{"status":"error","error":"Invalid path"}`, http.StatusBadRequest)
		return
	}

	m := targetPrefixRegex.FindStringSubmatch(r.URL.Path)
	if m == nil {
		http.Error(w, `{"status":"error","error":"Invalid target prefix"}`, 400)
		return
	}
	host, port, suffix := m[1], m[2], m[3]
	if suffix == "" {
		suffix = "/"
	}
	upstream := fmt.Sprintf("http://%s:%s", host, port)

	switch suffix {
	case "/api/v1/query":
		p.handleQuery(w, r, upstream, suffix)
	case "/api/v1/query_range":
		p.handleQueryRange(w, r, upstream, suffix)
	case "/api/v1/labels":
		p.handleLabels(w, r, upstream, suffix)
	default:
		if labelValuesRegex.MatchString(suffix) {
			parts := pathSplitRegex.Split(suffix, -1)
			label := parts[4]
			p.handleLabelValues(w, r, upstream, suffix, label)
			return
		}
		if DebugMode {
			log.Printf("Forwarding Unknown request: %s %s\n", r.Method, r.URL.Path)
		}
		forward(w, r, p.client, upstream+suffix)
	}
}
