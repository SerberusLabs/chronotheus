package proxy

import (
    "encoding/json"
    "io"
    "net/http"
)

// handleQuery → /api/v1/query
func (p *ChronoProxy) handleQuery(w http.ResponseWriter, r *http.Request, upstream, path string) {
    params := parseClientParams(r)
    requestedTf, command := detectSelectors(params)
    stripLabelFromParam(params, "query", "chrono_timeframe")
    stripLabelFromParam(params, "query", "command")

    // fetch all 5 windows
    all := fetchWindowsInstant(p, params, upstream+path, command)
    merged := dedupeSeries(all)

    avgList := buildLastMonthAverage(merged, false)
    merged = appendWithCommand(merged, avgList, command)

    curBySig, avgBySig := indexBySignature(merged, avgList)
    merged = appendCompare(merged, curBySig, avgBySig, command, false)
    merged = appendPercent(merged, curBySig, avgBySig, command, false)

    if requestedTf != "" {
        filtered := filterByTimeframe(merged, requestedTf)
        writeJSON(w, "vector", filtered)
        return
    }
    writeJSON(w, "vector", merged)
}

// handleQueryRange → /api/v1/query_range
func (p *ChronoProxy) handleQueryRange(w http.ResponseWriter, r *http.Request, upstream, path string) {
    params := parseClientParams(r)
    requestedTf, command := detectSelectors(params)
    stripLabelFromParam(params, "query", "chrono_timeframe")
    stripLabelFromParam(params, "query", "command")

    if params.Get("step") == "" {
        params.Set("step", "60")
    }

    all := fetchWindowsRange(p, params, upstream+path, command)
    merged := dedupeSeries(all)

    avgList := buildLastMonthAverage(merged, true)
    merged = appendWithCommand(merged, avgList, command)

    curBySig, avgBySig := indexBySignature(merged, avgList)
    merged = appendCompare(merged, curBySig, avgBySig, command, true)
    merged = appendPercent(merged, curBySig, avgBySig, command, true)

    if requestedTf != "" {
        filtered := filterByTimeframe(merged, requestedTf)
        writeJSON(w, "matrix", filtered)
        return
    }
    writeJSON(w, "matrix", merged)
}

// handleLabels → /api/v1/labels
func (p *ChronoProxy) handleLabels(w http.ResponseWriter, r *http.Request, upstream, path string) {
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
    out["data"] = data

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(out)
}

// handleLabelValues → /api/v1/label/{name}/values
func (p *ChronoProxy) handleLabelValues(w http.ResponseWriter, r *http.Request, upstream, path, label string) {
    if label == "chrono_timeframe" {
        writeJSONRaw(w, map[string]interface{}{
            "status": "success",
            "data":   append(append([]string{}, proxyTimeframes()...), "lastMonthAverage", "compareAgainstLast28", "percentCompareAgainstLast28"),
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
}
