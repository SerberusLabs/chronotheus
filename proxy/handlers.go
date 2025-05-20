// proxy/handlers.go
package proxy

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
    // Pre-compile frequently used regexps
    timeframeRegexp = regexp.MustCompile(`^chrono_timeframe="([^"]+)"$`)
    commandRegexp   = regexp.MustCompile(`^_command="([^"]+)"$`)
    
    // Pool for frequently allocated maps
    labelsPool = sync.Pool{
        New: func() interface{} {
            return make(map[string]interface{})
        },
    }
    
    // Default client timeout
    defaultTimeout = 10 * time.Second

)


// handleQuery implements /api/v1/query (instant).
func (p *ChronoProxy) handleQuery(w http.ResponseWriter, r *http.Request, upstream, path string) {
    if DebugMode {
        log.Printf("[DEBUG] handleQuery: %s %s", r.Method, r.URL.Path)
    }
    // 1) Merge and normalize
    params := parseClientParams(r)
    remapMatch(params)

    // 2) Pull out requestedTf and command
    requestedTf, command := extractSelectors(params)

    // 3) Strip them from the raw PromQL
    stripLabelFromParam(params, "query", "chrono_timeframe")
    stripLabelFromParam(params, "query", "command")

    // 4) Decide if we can optimize to a single raw window
    effProxy := p
    if isRawTf(requestedTf, p.timeframes) && command != "DONT_REMOVE_UNUSED_HISTORICS" {
        // find the index of that raw timeframe
        for i, tf := range p.timeframes {
            if tf == requestedTf {
                effProxy = &ChronoProxy{
                    offsets:    []int64{p.offsets[i]},
                    timeframes: []string{tf},
                    client:     p.client,
                }
                break
            }
        }
    }

	if DebugMode {
		log.Printf("[DEBUG] handleQuery: match[]=%v, query=%q, time=%q",
			params["match[]"], params.Get("query"), params.Get("time"))
		log.Printf("[DEBUG] fetchWindowsInstant → %s?%s", upstream+path, buildQueryString(params))
	}
    // 6) Fetch + shift
    all := fetchWindowsInstant(effProxy, params, upstream+path, command)
    
	if DebugMode {
		log.Printf("[DEBUG] fetched %d series", len(all))
	}

    // 7) Build synthetics
    merged := dedupeSeries(all)
    avg := buildLastMonthAverage(merged, false)
    merged = appendWithCommand(merged, avg, command)
    curM, avgM := indexBySignature(merged, avg)
    merged = appendCompare(merged, curM, avgM, command, false)
    merged = appendPercent(merged, curM, avgM, command, false)

    // 8) If they asked for a single timeframe, filter now
    if requestedTf != "" && command != "DONT_REMOVE_UNUSED_HISTORICS" {
        merged = filterByTimeframe(merged, requestedTf)
    }

    // 9) Return
    writeJSON(w, "vector", merged)
    if DebugMode {
        log.Printf("[DEBUG] handleQuery written to requester: %d series returned", len(merged))
    }

    // Compress response if client accepts it
    if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
        w.Header().Set("Content-Encoding", "gzip")
        gz := gzip.NewWriter(w)
        defer gz.Close()
        json.NewEncoder(gz).Encode(merged) 
    } else {
        json.NewEncoder(w).Encode(merged) 
    }
}

// handleQueryRange implements /api/v1/query_range (matrix).
func (p *ChronoProxy) handleQueryRange(w http.ResponseWriter, r *http.Request, upstream, path string) {
	
	if DebugMode {
		log.Printf("[DEBUG] handleQueryRange: %s %s", r.Method, r.URL.Path)
	}

    params := parseClientParams(r)
    remapMatch(params)

    requestedTf, command := extractSelectors(params)
    stripLabelFromParam(params, "query", "chrono_timeframe")
    stripLabelFromParam(params, "query", "command")
    if params.Get("step") == "" {
        params.Set("step", "60")
    }

    effProxy := p
    if isRawTf(requestedTf, p.timeframes) && command != "DONT_REMOVE_UNUSED_HISTORICS" {
        for i, tf := range p.timeframes {
            if tf == requestedTf {
                effProxy = &ChronoProxy{
                    offsets:    []int64{p.offsets[i]},
                    timeframes: []string{tf},
                    client:     p.client,
                }
                break
            }
        }
    }

	if DebugMode {
		log.Printf("[DEBUG] handleQueryRange: match[]=%v, query=%q, start=%q, end=%q, step=%q",
			params["match[]"], params.Get("query"), params.Get("start"), params.Get("end"), params.Get("step"))
		log.Printf("[DEBUG] fetchWindowsRange → %s?%s", upstream+path, buildQueryString(params))
	}

    all := fetchWindowsRange(effProxy, params, upstream+path, command)
   
	if DebugMode {
		log.Printf("[DEBUG] fetched %d series", len(all))
	}

    merged := dedupeSeries(all)
    avg := buildLastMonthAverage(merged, true)
    merged = appendWithCommand(merged, avg, command)
    curM, avgM := indexBySignature(merged, avg)
    merged = appendCompare(merged, curM, avgM, command, true)
    merged = appendPercent(merged, curM, avgM, command, true)

    if requestedTf != "" && command != "DONT_REMOVE_UNUSED_HISTORICS" {
        merged = filterByTimeframe(merged, requestedTf)
    }

    writeJSON(w, "matrix", merged)
	if DebugMode {
		log.Printf("[DEBUG] handleQueryRange written to requester: %d series returned", len(merged))
	}
}

// handleLabels advertises chrono_timeframe + _command
func (p *ChronoProxy) handleLabels(w http.ResponseWriter, r *http.Request, upstream, path string) {

	if DebugMode {
		log.Printf("[DEBUG] handleLabels: %s %s", r.Method, r.URL.Path)
	}

    // Get params and clean them
    params := parseClientParams(r)
    stripLabelFromParam(params, "match", "chrono_timeframe") 
    stripLabelFromParam(params, "match", "command")
    remapMatch(params)

    // Create request with timeout
    ctx, cancel := context.WithTimeout(r.Context(), defaultTimeout)
    defer cancel()

    req, err := http.NewRequestWithContext(ctx, "GET", upstream+path+"?"+buildQueryString(params), nil)
    if err != nil {
        http.Error(w, `{"status":"error","error":"Failed to create request"}`, http.StatusInternalServerError)
        return
    }

    resp, err := p.client.Do(req)
    if err != nil {
        http.Error(w, `{"status":"error","error":"Upstream request failed"}`, http.StatusBadGateway)
        return
    }
    defer resp.Body.Close()

    // Get map from pool
    out := labelsPool.Get().(map[string]interface{})
    defer func() {
        // Clear and return to pool
        for k := range out {
            delete(out, k)
        }
        labelsPool.Put(out)
    }()

    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        http.Error(w, `{"status":"error","error":"Invalid JSON from upstream"}`, http.StatusBadGateway)
        return
    }

    // Process data array
    data, ok := out["data"].([]interface{})
    if !ok {
        data = make([]interface{}, 0, 2) // Pre-allocate for our 2 synthetic labels
        out["status"] = "success"
    }

    // Use single pass to check both labels
    foundTimeframe, foundCommand := false, false
    for _, v := range data {
        if s, ok := v.(string); ok {
            switch s {
            case "chrono_timeframe":
                foundTimeframe = true
            case "_command":
                foundCommand = true
            }
        }
    }

    // Append only missing labels
    if !foundTimeframe {
        data = append(data, "chrono_timeframe")
    }
    if !foundCommand {
        data = append(data, "_command")
    }
    out["data"] = data

    // Write response
    w.Header().Set("Content-Type", "application/json")
    if err := json.NewEncoder(w).Encode(out); err != nil {
        log.Printf("[ERROR] Failed to encode response: %v", err)
    }

    if DebugMode {
        log.Printf("[DEBUG] handleLabels: written response with %d labels", len(data))
    }
}

// handleLabelValues serves synthetic label values
func (p *ChronoProxy) handleLabelValues(w http.ResponseWriter, r *http.Request, upstream, path, label string) {

	if DebugMode {
		log.Printf("[DEBUG] handleLabelValues: %s %s", r.Method, r.URL.Path)
	}

    switch label {
    case "chrono_timeframe":
        writeJSONRaw(w, map[string]interface{}{
            "status": "success",
            "data":   append(proxyTimeframes(),
                "lastMonthAverage", "compareAgainstLast28", "percentCompareAgainstLast28"),
        })
        return
    case "_command":
        writeJSONRaw(w, map[string]interface{}{
            "status": "success",
            "data":   []string{"", "DONT_REMOVE_UNUSED_HISTORICS"},
        })
        return
    }

    params := parseClientParams(r)
    stripLabelFromParam(params, "match", "chrono_timeframe")
    stripLabelFromParam(params, "match", "command")
    remapMatch(params)

    u := upstream + path + "?" + buildQueryString(params)
    resp, err := p.client.Get(u)
    if err != nil {
        http.Error(w, `{"status":"error","error":"Upstream request failed"}`, http.StatusBadGateway)
        return
    }
    defer resp.Body.Close()

    w.Header().Set("Content-Type", "application/json")
    io.Copy(w, resp.Body)
	if DebugMode {
		log.Printf("[DEBUG] handleLabelValues written to requester")
	}
}

// --- Helper to pull out both chrono_timeframe & _command from match[] or inline ---
func extractSelectors(vals url.Values) (string, string) {
    tf, cmd := "", ""
    if vs, ok := vals["match[]"]; ok {
        for i, m := range vs {
            if timeframeRegexp.MatchString(m) {
                tf = timeframeRegexp.FindStringSubmatch(m)[1]
                vals["match[]"] = append(vs[:i], vs[i+1:]...)
                break
            }
        }
        for i, m := range vs {
            if commandRegexp.MatchString(m) {
                cmd = commandRegexp.FindStringSubmatch(m)[1]
                vals["match[]"] = append(vals["match[]"][:i], vals["match[]"][i+1:]...)
                break
            }
        }
    }
    tf2, cmd2 := detectSelectors(vals)
    if tf == "" {
        tf = tf2
    }
    if cmd == "" {
        cmd = cmd2
    }
    return tf, cmd
}

// isRawTf returns true if tf is one of the raw 0/7/14/21/28-day timeframes
func isRawTf(tf string, raws []string) bool {
    for _, r := range raws {
        if tf == r {
            return true
        }
    }
    return false
}

