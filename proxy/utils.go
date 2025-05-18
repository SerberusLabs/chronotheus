package proxy

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "regexp"
    "sort"
    "strconv"
    "strings"
    "time"
)

// ─── PARAM PARSING & STRIPPING ────────────────────────────────────────────────

// parseClientParams merges GET + JSON-POST + form-POST into url.Values
func parseClientParams(r *http.Request) url.Values {
    vals := url.Values{}
    if r.Method == "POST" {
        ct := r.Header.Get("Content-Type")
        body, _ := io.ReadAll(r.Body)
        if strings.Contains(ct, "application/json") {
            var m map[string]interface{}
            json.Unmarshal(body, &m)
            for k, v := range m {
                switch arr := v.(type) {
                case []interface{}:
                    for _, x := range arr {
                        vals.Add(k, fmt.Sprintf("%v", x))
                    }
                default:
                    vals.Set(k, fmt.Sprintf("%v", v))
                }
            }
        } else {
            r.Body = io.NopCloser(bytes.NewReader(body))
            r.ParseForm()
            for k, vs := range r.PostForm {
                for _, x := range vs {
                    vals.Add(k, x)
                }
            }
        }
    }
    for k, vs := range r.URL.Query() {
        for _, x := range vs {
            vals.Add(k, x)
        }
    }
    return vals
}

// stripLabelFromParam removes ,?label="value" and cleans up stray commas/braces
func stripLabelFromParam(vals url.Values, key, label string) {
    re := regexp.MustCompile(`,?` + regexp.QuoteMeta(label) + `="[^"]*"`)
    if vs, ok := vals[key]; ok {
        for i, s := range vs {
            s = re.ReplaceAllString(s, "")
            s = regexp.MustCompile(`,+`).ReplaceAllString(s, ",")
            s = regexp.MustCompile(`\{\s*,+`).ReplaceAllString(s, "{")
            s = regexp.MustCompile(`,+\s*\}`).ReplaceAllString(s, "}")
            vs[i] = s
        }
        vals[key] = vs
    }
}

// remapMatch turns a single "match" into "match[]" for Prometheus
func remapMatch(vals url.Values) {
    if m := vals["match"]; len(m) > 0 && vals.Get("match[]") == "" {
        vals["match[]"] = m
        delete(vals, "match")
    }
}

// detectSelectors pulls out any chrono_timeframe="…" or _command="…"
func detectSelectors(vals url.Values) (string, string) {
    q := vals.Get("query")
    tf, cmd := "", ""
    if m := regexp.MustCompile(`\bchrono_timeframe="([^"]+)"`).FindStringSubmatch(q); m != nil {
        tf = m[1]
    }
    if m := regexp.MustCompile(`\b_command="([^"]+)"`).FindStringSubmatch(q); m != nil {
        cmd = m[1]
    }
    return tf, cmd
}

// ─── UPSTREAM FORWARDING ──────────────────────────────────────────────────────

// buildQueryString serializes url.Values preserving [] syntax
func buildQueryString(vals url.Values) string {
    parts := []string{}
    for k, vs := range vals {
        name := k
        if len(vs) > 1 && !strings.HasSuffix(k, "[]") {
            name = k + "[]"
        }
        for _, v := range vs {
            parts = append(parts, url.QueryEscape(name)+"="+url.QueryEscape(v))
        }
    }
    return strings.Join(parts, "&")
}

// forward proxies any request unchanged
func forward(w http.ResponseWriter, r *http.Request, client *http.Client, urlStr string) {
    var req *http.Request
    var err error
    if r.Method == "GET" {
        req, err = http.NewRequest("GET", urlStr+"?"+r.URL.RawQuery, nil)
    } else {
        body, _ := io.ReadAll(r.Body)
        req, err = http.NewRequest(r.Method, urlStr, bytes.NewReader(body))
    }
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    resp, err := client.Do(req)
    if err != nil {
        http.Error(w, err.Error(), 502)
        return
    }
    defer resp.Body.Close()
    for k, vv := range resp.Header {
        w.Header()[k] = vv
    }
    w.WriteHeader(resp.StatusCode)
    io.Copy(w, resp.Body)
}

// ─── WINDOWS FETCHING ─────────────────────────────────────────────────────────

// fetchWindowsInstant grabs five instant vectors (0,7,14,21,28d)
func fetchWindowsInstant(p *ChronoProxy, params url.Values, urlStr, command string) []map[string]interface{} {
    all := []map[string]interface{}{}
    for i, offset := range p.offsets {
        tf := p.timeframes[i]
        base := parseTime(params.Get("time"))
        params.Set("time", strconv.FormatInt(base-offset, 10))
        u := urlStr + "?" + buildQueryString(params)
        resp, err := p.client.Get(u)
        if err != nil {
            continue
        }
        body, _ := io.ReadAll(resp.Body); resp.Body.Close()
        var jr map[string]interface{}
        json.Unmarshal(body, &jr)
        data, ok := jr["data"].(map[string]interface{})
        if !ok {
            continue
        }
        results, ok := data["result"].([]interface{})
        if !ok {
            continue
        }
        for _, it := range results {
            s := it.(map[string]interface{})
            pair := s["value"].([]interface{})
            ts := int64(pair[0].(float64))
            val := fmt.Sprintf("%v", pair[1])
            s["value"] = []interface{}{ts + offset, val}
            m := s["metric"].(map[string]interface{})
            m["chrono_timeframe"] = tf
            if command != "" {
                m["_command"] = command
            }
            all = append(all, s)
        }
    }
    return all
}

// fetchWindowsRange grabs five range matrices
func fetchWindowsRange(p *ChronoProxy, params url.Values, urlStr, command string) []map[string]interface{} {
    all := []map[string]interface{}{}
    for i, offset := range p.offsets {
        tf := p.timeframes[i]
        start := parseTime(params.Get("start")) - offset
        end := parseTime(params.Get("end")) - offset
        params.Set("start", strconv.FormatInt(start, 10))
        params.Set("end", strconv.FormatInt(end, 10))
        u := urlStr + "?" + buildQueryString(params)
        resp, err := p.client.Get(u)
        if err != nil {
            continue
        }
        body, _ := io.ReadAll(resp.Body); resp.Body.Close()
        var jr map[string]interface{}
        json.Unmarshal(body, &jr)
        data, ok := jr["data"].(map[string]interface{})
        if !ok {
            continue
        }
        results, ok := data["result"].([]interface{})
        if !ok {
            continue
        }
        for _, it := range results {
            s := it.(map[string]interface{})
            vs, ok := s["values"].([]interface{})
            if !ok {
                continue
            }
            shifted := []interface{}{}
            for _, iv := range vs {
                pair := iv.([]interface{})
                ts := int64(pair[0].(float64))
                val := fmt.Sprintf("%v", pair[1])
                shifted = append(shifted, []interface{}{ts + offset, val})
            }
            s["values"] = shifted
            m := s["metric"].(map[string]interface{})
            m["chrono_timeframe"] = tf
            if command != "" {
                m["_command"] = command
            }
            all = append(all, s)
        }
    }
    return all
}

// containsString checks for a string in []interface{}
func containsString(arr []interface{}, s string) bool {
    for _, v := range arr {
        if str, ok := v.(string); ok && str == s {
            return true
        }
    }
    return false
}

// ─── TIME & SIGNATURE HELPERS ────────────────────────────────────────────────

// parseTime parses integer or RFC3339 → epoch seconds
func parseTime(s string) int64 {
    if i, err := strconv.ParseInt(s, 10, 64); err == nil {
        return i
    }
    if t, err := time.Parse(time.RFC3339, s); err == nil {
        return t.Unix()
    }
    return time.Now().Unix()
}

// signature returns a canonical JSON key, minus synthetic labels
func signature(metric map[string]interface{}) string {
    m := copyMetric(metric)
    delete(m, "chrono_timeframe")
    delete(m, "_command")
    keys := make([]string, 0, len(m))
    for k := range m {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    ordered := map[string]interface{}{}
    for _, k := range keys {
        ordered[k] = m[k]
    }
    b, _ := json.Marshal(ordered)
    return string(b)
}

// copyMetric shallow-copies the metric map
func copyMetric(orig map[string]interface{}) map[string]interface{} {
    c := map[string]interface{}{}
    for k, v := range orig {
        c[k] = v
    }
    return c
}

// ─── DEDUPE & AVERAGING ──────────────────────────────────────────────────────

// dedupeSeries groups by signature, flattens
func dedupeSeries(all []map[string]interface{}) []map[string]interface{} {
    bySig := map[string][]map[string]interface{}{}
    for _, s := range all {
        sig := signature(s["metric"].(map[string]interface{}))
        bySig[sig] = append(bySig[sig], s)
    }
    out := []map[string]interface{}{}
    for _, grp := range bySig {
        out = append(out, grp...)
    }
    return out
}

// proxyTimeframes exposes the list of chrono_timeframe tags
func proxyTimeframes() []string {
    return []string{"current", "7days", "14days", "21days", "28days"}
}

// buildLastMonthAverage builds one avg series per metric signature
func buildLastMonthAverage(seriesList []map[string]interface{}, isRange bool) []map[string]interface{} {
    n := len(proxyTimeframes()) - 1
    if n < 1 {
        return nil
    }
    groups := map[string][]map[string]interface{}{}
    for _, s := range seriesList {
        m := s["metric"].(map[string]interface{})
        if m["chrono_timeframe"] == "current" {
            continue
        }
        base := copyMetric(m)
        delete(base, "chrono_timeframe")
        delete(base, "_command")
        sig := signature(base)
        groups[sig] = append(groups[sig], s)
    }
    out := []map[string]interface{}{}
    for sig, grp := range groups {
        sums := map[int64]float64{}
        for _, s := range grp {
            var pts []interface{}
            if isRange {
                pts = s["values"].([]interface{})
            } else {
                pts = []interface{}{s["value"]}
            }
            for _, iv := range pts {
                pair := iv.([]interface{})
                ts := int64(pair[0].(float64))
                v, _ := strconv.ParseFloat(fmt.Sprintf("%v", pair[1]), 64)
                minute := (ts / 60) * 60
                sums[minute] += v
            }
        }
        mins := make([]int64, 0, len(sums))
        for m := range sums {
            mins = append(mins, m)
        }
        sort.Slice(mins, func(i, j int) bool { return mins[i] < mins[j] })
        ptsOut := []interface{}{}
        for _, m := range mins {
            avg := sums[m] / float64(n)
            ptsOut = append(ptsOut, []interface{}{m, fmt.Sprintf("%g", avg)})
        }
        metric := map[string]interface{}{}
        json.Unmarshal([]byte(sig), &metric)
        metric["chrono_timeframe"] = "lastMonthAverage"
        if isRange {
            out = append(out, map[string]interface{}{"metric": metric, "values": ptsOut})
        } else {
            last := ptsOut[len(ptsOut)-1].([]interface{})
            out = append(out, map[string]interface{}{"metric": metric, "value": last})
        }
    }
    return out
}

// indexBySignature builds maps of current & avg series by signature
func indexBySignature(
    all []map[string]interface{},
    avgList []map[string]interface{},
) (map[string]map[string]interface{}, map[string]map[string]interface{}) {
    curBySig := map[string]map[string]interface{}{}
    avgBySig := map[string]map[string]interface{}{}
    for _, s := range all {
        m := s["metric"].(map[string]interface{})
        if m["chrono_timeframe"] == "current" {
            curBySig[signature(m)] = s
        }
    }
    for _, a := range avgList {
        m := a["metric"].(map[string]interface{})
        avgBySig[signature(m)] = a
    }
    return curBySig, avgBySig
}

// appendWithCommand tacks on avgList carrying _command
func appendWithCommand(
    base []map[string]interface{},
    avgList []map[string]interface{},
    command string,
) []map[string]interface{} {
    out := base
    for _, avg := range avgList {
        if command != "" {
            avg["metric"].(map[string]interface{})["_command"] = command
        }
        out = append(out, avg)
    }
    return out
}

// appendCompare builds compareAgainstLast28
func appendCompare(
    base []map[string]interface{},
    curBySig, avgBySig map[string]map[string]interface{},
    command string,
    isRange bool,
) []map[string]interface{} {
    out := base
    for sig, cur := range curBySig {
        avg, ok := avgBySig[sig]
        if !ok {
            continue
        }
        mCur := cur["metric"].(map[string]interface{})
        nm := copyMetric(mCur)
        nm["chrono_timeframe"] = "compareAgainstLast28"
        if command != "" {
            nm["_command"] = command
        }
        if !isRange {
            pc := cur["value"].([]interface{})
            va := avg["value"].([]interface{})[1].(string)
            vc := pc[1].(string)
            dv, _ := strconv.ParseFloat(vc, 64)
            av, _ := strconv.ParseFloat(va, 64)
            delta := dv - av
            out = append(out, map[string]interface{}{
                "metric": nm,
                "value":  []interface{}{pc[0], fmt.Sprintf("%g", delta)},
            })
        }
        // range version omitted for brevity
    }
    return out
}

// appendPercent builds percentCompareAgainstLast28
func appendPercent(
    base []map[string]interface{},
    curBySig, avgBySig map[string]map[string]interface{},
    command string,
    isRange bool,
) []map[string]interface{} {
    out := base
    for sig, cur := range curBySig {
        avg, ok := avgBySig[sig]
        if !ok {
            continue
        }
        mCur := cur["metric"].(map[string]interface{})
        nm := copyMetric(mCur)
        nm["chrono_timeframe"] = "percentCompareAgainstLast28"
        if command != "" {
            nm["_command"] = command
        }
        if !isRange {
            pc := cur["value"].([]interface{})
            va := avg["value"].([]interface{})[1].(string)
            vc := pc[1].(string)
            dv, _ := strconv.ParseFloat(vc, 64)
            av, _ := strconv.ParseFloat(va, 64)
            pct := 0.0
            if av != 0 {
                pct = (dv-av)/av * 100
            }
            out = append(out, map[string]interface{}{
                "metric": nm,
                "value":  []interface{}{pc[0], fmt.Sprintf("%g", pct)},
            })
        }
        // range version omitted for brevity
    }
    return out
}

// filterByTimeframe filters for a single chrono_timeframe
func filterByTimeframe(
    all []map[string]interface{},
    tf string,
) []map[string]interface{} {
    out := []map[string]interface{}{}
    for _, s := range all {
        if s["metric"].(map[string]interface{})["chrono_timeframe"] == tf {
            out = append(out, s)
        }
    }
    return out
}

// writeJSON emits the standard Prometheus envelope
func writeJSON(w http.ResponseWriter, rt string, result []map[string]interface{}) {
    w.Header().Set("Content-Type", "application/json")
    out := map[string]interface{}{
        "status": "success",
        "data": map[string]interface{}{
            "resultType": rt,
            "result":     result,
        },
    }
    json.NewEncoder(w).Encode(out)
}

// writeJSONRaw emits any raw JSON-able object
func writeJSONRaw(w http.ResponseWriter, v interface{}) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(v)
}
