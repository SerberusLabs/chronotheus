// proxy/handlers.go
package proxy

import (
    "encoding/json"
    "io"
    "log"
    "net/url"
    "net/http"
    "regexp"
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
		log.Printf("[DEBUG] handleLabels written to requester: %v", out)
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
		log.Printf("[DEBUG] handleLabelValues written to requester: %s", label)
	}
}

// --- Helper to pull out both chrono_timeframe & _command from match[] or inline ---
func extractSelectors(vals url.Values) (string, string) {
    tf, cmd := "", ""
    if vs, ok := vals["match[]"]; ok {
        for i, m := range vs {
            if re := regexp.MustCompile(`^chrono_timeframe="([^"]+)"$`); re.MatchString(m) {
                tf = re.FindStringSubmatch(m)[1]
                vals["match[]"] = append(vs[:i], vs[i+1:]...)
                break
            }
        }
        for i, m := range vs {
            if re := regexp.MustCompile(`^_command="([^"]+)"$`); re.MatchString(m) {
                cmd = re.FindStringSubmatch(m)[1]
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
