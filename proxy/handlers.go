// proxy/handlers.go
package proxy

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
)

// handleQuery implements /api/v1/query (instant).
func (p *ChronoProxy) handleQuery(w http.ResponseWriter, r *http.Request, upstream, path string) {
    if DebugMode {
        log.Printf("[DEBUG] handleQuery: %s %s", r.Method, r.URL.Path)
    }

    params := parseClientParams(r)
    remapMatch(params)

    requestedTf, command := extractSelectors(params)
    stripLabelFromParam(params, "query", "chrono_timeframe")
    stripLabelFromParam(params, "query", "command")

    var merged []map[string]interface{}

    // Case 1: No timeframe specified - return everything
    if requestedTf == "" {
        all := fetchWindowsInstant(p, params, upstream+path, command)
        merged = dedupeSeries(all)
        
        // Also build synthetic timeframes
        avg := buildLastMonthAverage(merged, false)
        curM, avgM := indexBySignature(merged, avg)
        
        merged = append(merged, avg...)
        merged = append(merged, appendCompare([]map[string]interface{}{}, curM, avgM, "", false)...)
        merged = append(merged, appendPercent([]map[string]interface{}{}, curM, avgM, "", false)...)
    } else if command == "DONT_REMOVE_UNUSED_HISTORICS" {
        // Case 2: Keep all historics
        all := fetchWindowsInstant(p, params, upstream+path, command)
        merged = dedupeSeries(all)
    } else if requestedTf == "lastMonthAverage" || 
              requestedTf == "compareAgainstLast28" || 
              requestedTf == "percentCompareAgainstLast28" {
        // Case 3: Synthetic timeframes - need all data to calculate
        all := fetchWindowsInstant(p, params, upstream+path, command)
        merged = dedupeSeries(all)
        
        avg := buildLastMonthAverage(merged, false)
        curM, avgM := indexBySignature(merged, avg)
        
        if requestedTf == "lastMonthAverage" {
            merged = avg
        } else if requestedTf == "compareAgainstLast28" {
            merged = appendCompare([]map[string]interface{}{}, curM, avgM, "", false)
        } else {
            merged = appendPercent([]map[string]interface{}{}, curM, avgM, "", false)
        }
    } else {
        // Case 4: Raw timeframe - only fetch that specific window
        for i, tf := range p.timeframes {
            if tf == requestedTf {
                effProxy := &ChronoProxy{
                    offsets:    []int64{p.offsets[i]},
                    timeframes: []string{tf},
                    client:     p.client,
                }
                all := fetchWindowsInstant(effProxy, params, upstream+path, command)
                merged = dedupeSeries(all)
                break
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

// handleQueryRange implements /api/v1/query_range (matrix).
func (p *ChronoProxy) handleQueryRange(w http.ResponseWriter, r *http.Request, upstream, path string) {
    if DebugMode {
        log.Printf("[DEBUG] handleQueryRange: %s %s", r.Method, r.URL.Path)
    }

    params := parseClientParams(r)
    remapMatch(params)

    requestedTf, command := extractSelectors(params)
    
    if DebugMode {
        log.Printf("Selectors are(TF:'%s', command: '%s')", requestedTf,command)
    }

    stripLabelFromParam(params, "query", "chrono_timeframe")
    stripLabelFromParam(params, "query", "command")
    if params.Get("step") == "" {
        params.Set("step", "60")
    }

    var merged []map[string]interface{}

    // Case 1: No timeframe specified - return everything
    if requestedTf == "" {
        all := fetchWindowsRange(p, params, upstream+path, command)
        merged = dedupeSeries(all)
        
        // Also build synthetic timeframes
        avg := buildLastMonthAverage(merged, true)
        curM, avgM := indexBySignature(merged, avg)
        
        merged = append(merged, avg...)
        merged = append(merged, appendCompare([]map[string]interface{}{}, curM, avgM, "", true)...)
        merged = append(merged, appendPercent([]map[string]interface{}{}, curM, avgM, "", true)...)
    } else if command == "DONT_REMOVE_UNUSED_HISTORICS" {
        // Case 2: Keep all historics
        all := fetchWindowsRange(p, params, upstream+path, command)
        merged = dedupeSeries(all)
    } else if requestedTf == "lastMonthAverage" || 
              requestedTf == "compareAgainstLast28" || 
              requestedTf == "percentCompareAgainstLast28" {
        // Case 3: Synthetic timeframes - need all data to calculate
        all := fetchWindowsRange(p, params, upstream+path, command)
        merged = dedupeSeries(all)
        
        avg := buildLastMonthAverage(merged, true)
        curM, avgM := indexBySignature(merged, avg)
        
        if requestedTf == "lastMonthAverage" {
            merged = avg
        } else if requestedTf == "compareAgainstLast28" {
            merged = appendCompare([]map[string]interface{}{}, curM, avgM, "", true)
        } else {
            merged = appendPercent([]map[string]interface{}{}, curM, avgM, "", true)
        }
    } else {
        // Case 4: Raw timeframe - only fetch that specific window
        for i, tf := range p.timeframes {
            if tf == requestedTf {
                effProxy := &ChronoProxy{
                    offsets:    []int64{p.offsets[i]},
                    timeframes: []string{tf},
                    client:     p.client,
                }
                all := fetchWindowsRange(effProxy, params, upstream+path, command)
                merged = dedupeSeries(all)
                break
            }
        }
    }

    // Filter by requested timeframe if specified
    if DebugMode {
        log.Printf("Entering cleanup process (TF is %s)", requestedTf)
    }
    if requestedTf != "" && command != "DONT_REMOVE_UNUSED_HISTORICS" {
        if DebugMode {
            log.Printf("Conditional OK")
        }
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
    
    if DebugMode {
        log.Printf("[DEBUG] extractSelectors checking match[] values: %v", vals["match[]"])
    }

    if vs, ok := vals["match[]"]; ok {
        for i, m := range vs {
            if re := regexp.MustCompile(`^chrono_timeframe="([^"]+)"$`); re.MatchString(m) {
                tf = re.FindStringSubmatch(m)[1]
                vals["match[]"] = append(vs[:i], vs[i+1:]...)
                if DebugMode {
                    log.Printf("[DEBUG] Found timeframe in match[]: %s", tf)
                }
                break
            }
        }
        for i, m := range vs {
            if re := regexp.MustCompile(`^_command="([^"]+)"$`); re.MatchString(m) {
                cmd = re.FindStringSubmatch(m)[1]
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



