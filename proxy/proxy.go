package proxy

import (
    "fmt"
    "net/http"
    "regexp"
	"log"
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
    re := regexp.MustCompile(`^/([^_/]+)_(\d+)(/.*)?$`)
    m := re.FindStringSubmatch(r.URL.Path)
    if m == nil {
        http.Error(w, `{"status":"error","error":"Invalid target prefix"}`, 400)
        return
    }
    host, port, suffix := m[1], m[2], m[3]
    if suffix == "" {
        suffix = "/"
    }
    upstream := fmt.Sprintf("http://%s:%s", host, port)

    switch {
    case suffix == "/api/v1/query" && (r.Method == "GET" || r.Method == "POST"):
        p.handleQuery(w, r, upstream, suffix)

    case suffix == "/api/v1/query_range" && (r.Method == "GET" || r.Method == "POST"):
        p.handleQueryRange(w, r, upstream, suffix)

    case suffix == "/api/v1/labels" && (r.Method == "GET" || r.Method == "POST"):
        p.handleLabels(w, r, upstream, suffix)

    case regexp.MustCompile(`^/api/v1/label/[^/]+/values$`).MatchString(suffix) &&
        (r.Method == "GET" || r.Method == "POST"):
        parts := regexp.MustCompile(`/`).Split(suffix, -1)
        label := parts[4]
        p.handleLabelValues(w, r, upstream, suffix, label)

    default:
		if DebugMode {
			log.Printf("Forwarding Unknown request: %s %s\n", r.Method, r.URL.Path)
		}
        forward(w, r, p.client, upstream+suffix)
    }
}
