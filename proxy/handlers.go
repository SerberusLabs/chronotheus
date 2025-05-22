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

// proxy/handlers.go
package proxy

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"
)

// Welcome to the handler functions!! WOOOOOOO
// These are like the bouncers at a club - they decide who gets in and what happens to them.
// Quick map of what's where:
//   - handleQuery: Handles instant queries (like "what's happening RIGHT NOW")
//   - handleQueryRange: Same but for ranges (like "what happened in the last hour")
//   - handleLabels: Lists all our cool label options
//   - handleLabelValues: Shows what values each label can have
//
// The most interesting bit is how we handle timeframes:
//   - Raw timeframes: current, 7days, 14days, etc
//   - Synthetic timeframes: averages and comparisons we calculate
//   - Magic command DONT_REMOVE_UNUSED_HISTORICS to see ALL THE THINGS!

// handleQuery implements /api/v1/query endpoint for instant queries.
// Think of it as taking a snapshot of your metrics RIGHT NOW! 📸
//
// How it works:
// 1. Gets params and finds what timeframe you want (if any)
// 2. Based on what you asked for:
//    - No timeframe? You get everything + synthetics!
//    - Want historics? You get ALL the timeframes!
//    - Want averages? We'll do some mathematical magic
//    - Specific timeframe? You get just that one!
// 3. Filters out anything you don't want
// 4. Sends it back as JSON
func (p *ChronoProxy) handleQuery(w http.ResponseWriter, r *http.Request, upstream, path string) {
    if DebugMode {
        log.Printf("[DEBUG] handleQuery: %s %s", r.Method, r.URL.Path)
    }

    params := parseClientParams(r)
    remapMatch(params)

    requestedTf, command := extractSelectors(params)
    stripLabelFromParam(params, "query", "chrono_timeframe")
    stripLabelFromParam(params, "query", "command")

    // Pre-allocate merged slice with reasonable capacity
    initialCap := 100
    if command == "DONT_REMOVE_UNUSED_HISTORICS" {
        initialCap *= len(p.timeframes)
    }
    var merged []map[string]interface{}

    // Optimize for specific timeframe request
    if requestedTf != "" && requestedTf != "lastMonthAverage" && 
       requestedTf != "compareAgainstLast28" && requestedTf != "percentCompareAgainstLast28" {
        // Handle single timeframe request efficiently
        for i, tf := range p.timeframes {
            if tf == requestedTf {
                effProxy := &ChronoProxy{
                    offsets:    []int64{p.offsets[i]},
                    timeframes: []string{tf},
                    client:     p.client,
                }
                merged = fetchWindowsInstant(effProxy, params, upstream+path, command)
                break
            }
        }
    } else {
        // Handle full data fetch cases
        all := fetchWindowsInstant(p, params, upstream+path, command)
        if command == "DONT_REMOVE_UNUSED_HISTORICS" {
            merged = dedupeSeries(all)
        } else if requestedTf == "" {
            // Case 1: No timeframe specified - return everything with synthetics
            merged = dedupeSeries(all)
            avg := buildLastMonthAverage(merged, false)
            curM, avgM := indexBySignature(merged, avg)
            
            // Pre-allocate final slice
            finalCap := len(merged) + len(avg) + len(curM)*2
            result := make([]map[string]interface{}, len(merged), finalCap)
            copy(result, merged)
            
            result = append(result, avg...)
            result = append(result, appendCompare(nil, curM, avgM, "", false)...)
            result = append(result, appendPercent(nil, curM, avgM, "", false)...)
            merged = result
        } else {
            // Case 3: Synthetic timeframes
            merged = dedupeSeries(all)
            avg := buildLastMonthAverage(merged, false)
            curM, avgM := indexBySignature(merged, avg)
            
            switch requestedTf {
            case "lastMonthAverage":
                merged = avg
            case "compareAgainstLast28":
                merged = appendCompare(nil, curM, avgM, "", false)
            case "percentCompareAgainstLast28":
                merged = appendPercent(nil, curM, avgM, "", false)
            }
        }
    }

    // Filter by requested timeframe if specified
    if requestedTf != "" && command != "DONT_REMOVE_UNUSED_HISTORICS" {
        merged = filterByTimeframe(merged, requestedTf)
    }

    writeJSON(w, "vector", merged)
    if DebugMode {
        log.Printf("[DEBUG] handleQuery written to requester: %d series returned", len(merged))
    }
}

// handleQueryRange is like handleQuery's older brother (or sister, depends how it self identifies) - it handles ranges of time
// instead of just instant snapshots. Think "give me a graph" vs "give me a number".
//
// The flow is similar to handleQuery but with MORE DATA:
// 1. Gets your params (including how big of steps you want - defaults to 60s)
// 2. Figures out what timeframe you're interested in
// 3. Does all the same magic as handleQuery but with sequences instead of points
// 4. Returns a beautiful matrix of data points
func (p *ChronoProxy) handleQueryRange(w http.ResponseWriter, r *http.Request, upstream, path string) {
    if DebugMode {
        log.Printf("[DEBUG] handleQueryRange: %s %s", r.Method, r.URL.Path)
    }

    params := parseClientParams(r)
    remapMatch(params)

    requestedTf, command := extractSelectors(params)
    
    if DebugMode {
        log.Printf("Selectors are(TF:'%s', command: '%s')", requestedTf, command)
    }

    stripLabelFromParam(params, "query", "chrono_timeframe")
    stripLabelFromParam(params, "query", "command")
    if params.Get("step") == "" {
        params.Set("step", "60")
    }

    // Pre-allocate merged slice with reasonable capacity
    initialCap := 100
    if command == "DONT_REMOVE_UNUSED_HISTORICS" {
        initialCap *= len(p.timeframes)
    }
    merged := make([]map[string]interface{}, 0, initialCap)

    // Optimize for specific timeframe request
    if requestedTf != "" && requestedTf != "lastMonthAverage" && 
       requestedTf != "compareAgainstLast28" && requestedTf != "percentCompareAgainstLast28" {
        // Handle single timeframe request efficiently
        for i, tf := range p.timeframes {
            if tf == requestedTf {
                effProxy := &ChronoProxy{
                    offsets:    []int64{p.offsets[i]},
                    timeframes: []string{tf},
                    client:     p.client,
                }
                merged = fetchWindowsRange(effProxy, params, upstream+path, command)
                break
            }
        }
    } else {
        // Handle full data fetch cases
        all := fetchWindowsRange(p, params, upstream+path, command)
        if command == "DONT_REMOVE_UNUSED_HISTORICS" {
            merged = dedupeSeries(all)
        } else if requestedTf == "" {
            // Case 1: No timeframe specified - return everything with synthetics
            merged = dedupeSeries(all)
            avg := buildLastMonthAverage(merged, true)
            curM, avgM := indexBySignature(merged, avg)
            
            // Pre-allocate final slice
            finalCap := len(merged) + len(avg) + len(curM)*2
            result := make([]map[string]interface{}, len(merged), finalCap)
            copy(result, merged)
            
            result = append(result, avg...)
            result = append(result, appendCompare(nil, curM, avgM, "", true)...)
            result = append(result, appendPercent(nil, curM, avgM, "", true)...)
            merged = result
        } else {
            // Case 3: Synthetic timeframes
            merged = dedupeSeries(all)
            avg := buildLastMonthAverage(merged, true)
            curM, avgM := indexBySignature(merged, avg)
            
            switch requestedTf {
            case "lastMonthAverage":
                merged = avg
            case "compareAgainstLast28":
                merged = appendCompare(nil, curM, avgM, "", true)
            case "percentCompareAgainstLast28":
                merged = appendPercent(nil, curM, avgM, "", true)
            }
        }
    }

    // Filter by requested timeframe if specified
    if requestedTf != "" && command != "DONT_REMOVE_UNUSED_HISTORICS" {
        merged = filterByTimeframe(merged, requestedTf)
    }

    writeJSON(w, "matrix", merged)
    if DebugMode {
        log.Printf("[DEBUG] handleQueryRange written to requester: %d series returned", len(merged))
    }
}

// handleLabels is our menu board! 🎯
// It tells Prometheus what special labels we support (chrono_timeframe and _command).
// Think of it like those signs outside a club that say "Tonight's Special: Time Travel! 🕰️"
//
// How it works:
// 1. Gets the upstream labels (the regular ones)
// 2. Adds our cool custom labels to the list
// 3. Sends back the complete menu of options
func (p *ChronoProxy) handleLabels(w http.ResponseWriter, r *http.Request, upstream, path string) {

	if DebugMode {
		log.Printf("[DEBUG] handleLabels: %s %s", r.Method, r.URL.Path)
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

    var out map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&out)

    data, ok := out["data"].([]interface{})
    if !ok {
        data = []interface{}{}
        out["status"] = "success"
    }
    if !containsString(data, "chrono_timeframe") {
        data = append(data, "chrono_timeframe")
    }
    if !containsString(data, "_command") {
        data = append(data, "_command")
    }
    out["data"] = data

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(out)
	if DebugMode {
		log.Printf("[DEBUG] handleLabels written to requester")
	}
}

// Cache for label values with TTL
var (
    labelValuesCache    = make(map[string]labelValuesCacheEntry)
    labelValuesCacheMux sync.RWMutex
)

type labelValuesCacheEntry struct {
    data      []interface{}
    timestamp time.Time
}

const labelValuesCacheTTL = 5 * time.Minute

// handleLabelValues is like a vending machine for label values! 
// You put in a label name, it gives you all the possible values.
//
// Special cases:
// - chrono_timeframe: Returns all our time windows (raw + synthetic)
// - _command: Returns our magic commands (like DONT_REMOVE_UNUSED_HISTORICS)
// - anything else: Passes through to the upstream Prometheus
//
// Pro tip: This is how Grafana knows what values to show in dropdowns! 
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

    // Check cache first
    labelValuesCacheMux.RLock()
    if entry, ok := labelValuesCache[label]; ok && time.Since(entry.timestamp) < labelValuesCacheTTL {
        labelValuesCacheMux.RUnlock()
        writeJSONRaw(w, map[string]interface{}{
            "status": "success",
            "data":   entry.data,
        })
        return
    }
    labelValuesCacheMux.RUnlock()

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

    // Parse response to cache it
    var result map[string]interface{}
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        http.Error(w, `{"status":"error","error":"Invalid response from upstream"}`, http.StatusBadGateway)
        return
    }

    // Update cache
    if data, ok := result["data"].([]interface{}); ok {
        labelValuesCacheMux.Lock()
        labelValuesCache[label] = labelValuesCacheEntry{
            data:      data,
            timestamp: time.Now(),
        }
        labelValuesCacheMux.Unlock()
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(result)
    if DebugMode {
        log.Printf("[DEBUG] handleLabelValues written to requester")
    }
}

var (
    timeframeRegex = regexp.MustCompile(`^chrono_timeframe="([^"]+)"$`)
    commandRegex   = regexp.MustCompile(`^_command="([^"]+)"$`)
)

// extractSelectors efficiently extracts both chrono_timeframe & _command from match[] or inline
func extractSelectors(vals url.Values) (string, string) {
    tf, cmd := "", ""
    
    if DebugMode {
        log.Printf("[DEBUG] extractSelectors checking match[] values: %v", vals["match[]"])
    }

    if vs, ok := vals["match[]"]; ok {
        for i, m := range vs {
            if matches := timeframeRegex.FindStringSubmatch(m); matches != nil {
                tf = matches[1]
                vals["match[]"] = append(vs[:i], vs[i+1:]...)
                if DebugMode {
                    log.Printf("[DEBUG] Found timeframe in match[]: %s", tf)
                }
                break
            }
        }
        for i, m := range vs {
            if matches := commandRegex.FindStringSubmatch(m); matches != nil {
                cmd = matches[1]
                vals["match[]"] = append(vals["match[]"][:i], vals["match[]"][i+1:]...)
                if DebugMode {
                    log.Printf("[DEBUG] Found command in match[]: %s", cmd)
                }
                break
            }
        }
    }

    // Try inline detection if nothing found in match[]
    if tf == "" || cmd == "" {
        if DebugMode {
            log.Printf("[DEBUG] Checking inline selectors in query: %s", vals.Get("query"))
        }
        tf2, cmd2 := detectSelectors(vals)
        if tf == "" {
            tf = tf2
        }
        if cmd == "" {
            cmd = cmd2
        }
    }

    if DebugMode {
        log.Printf("[DEBUG] Final selector values - timeframe: '%s', command: '%s'", tf, cmd)
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