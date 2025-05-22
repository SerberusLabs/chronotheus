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
	"net/http"
	"regexp"
)

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
}

// NewChronoProxy is our time window factory! 
// Sets up our magical time windows for comparing data:
// - current: What's happening right now
// - 7days:   What happened last week
// - 14days:  Two weeks ago
// - 21days:  Three weeks ago
// - 28days:  A whole month back!
//
// Pro tip: These windows let us spot patterns and trends!
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

// ServeHTTP is Herr Traffik Direktor! 
// It looks at incoming requests and sends them to the right handler:
// - /api/v1/query:        Want a snapshot? This way!
// - /api/v1/query_range:  Need a graph? Over here!
// - /api/v1/labels:       Looking for label options? Follow me!
// - /api/v1/label/.../values: Need specific values? Got you covered!
// - anything else:        Just passing through!
//
// Think of it like a helpful concierge who knows exactly where everything is!
//
// Pro tip: Watch the debug logs to see it in action - it's quite chatty!
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
