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
	"log"
)

// ─── PARAMS & STRIPPING ─────────────────────────────────────────────────────────-

// parseClientParams merges GET and POST parameters into url.Values
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

// detectSelectors finds chrono_timeframe and _command labels in inline `query`
func detectSelectors(vals url.Values) (string, string) {
    q := vals.Get("query")
    tf, cmd := "", ""
    if m := regexp.MustCompile(`\\bchrono_timeframe="([^"]+)"`).FindStringSubmatch(q); m != nil {
        tf = m[1]
    }
    if m := regexp.MustCompile(`\\b_command="([^"]+)"`).FindStringSubmatch(q); m != nil {
        cmd = m[1]
    }
    return tf, cmd
}

// stripLabelFromParam removes a label matcher from a given parameter
func stripLabelFromParam(vals url.Values, key, label string) {
    re := regexp.MustCompile(`,?` + regexp.QuoteMeta(label) + `="[^"]*"`)
    if vs, ok := vals[key]; ok {
        for i, s := range vs {
            s = re.ReplaceAllString(s, "")
            s = regexp.MustCompile(`,+`).ReplaceAllString(s, ",")
            s = regexp.MustCompile(`{\\s*,+`).ReplaceAllString(s, "{")
            s = regexp.MustCompile(`,+\\s*}`).ReplaceAllString(s, "}")
            vs[i] = s
        }
        vals[key] = vs
    }
}

// remapMatch ensures match[] is used rather than match
func remapMatch(vals url.Values) {
    if m := vals["match"]; len(m) > 0 && vals.Get("match[]") == "" {
        vals["match[]"] = m
        delete(vals, "match")
    }
}

// ─── FORWARD / BUILD QS ───────────────────────────────────────────────────────

// buildQueryString constructs a URL-encoded query string
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

// forward proxies all other requests unchanged
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

// ─── FETCH WINDOWS ────────────────────────────────────────────────────────────

type instantRes struct {
    Data struct {
        Result []struct {
            Metric map[string]interface{} `json:"metric"`
            Value  [2]interface{}         `json:"value"`
        } `json:"result"`
    } `json:"data"`
}

func fetchWindowsInstant(p *ChronoProxy, params url.Values, endpoint, command string) []map[string]interface{} {
    var all []map[string]interface{}
    for i, offset := range p.offsets {
        tf := p.timeframes[i]
        base := parseTime(params.Get("time"))
        params.Set("time", strconv.FormatInt(base-offset, 10))

        u := endpoint + "?" + buildQueryString(params)
        resp, err := p.client.Get(u)
        if err != nil {
            continue
        }
        body, _ := io.ReadAll(resp.Body)
        resp.Body.Close()

        var jr instantRes
        if err := json.Unmarshal(body, &jr); err != nil {
            continue
        }
        for _, s := range jr.Data.Result {
            tsf := s.Value[0].(float64)
            ts := int64(tsf) + offset
            val := fmt.Sprintf("%v", s.Value[1])

            m := copyMetric(s.Metric)
            m["chrono_timeframe"] = tf
            if command != "" {
                m["_command"] = command
            }

            all = append(all, map[string]interface{}{
                "metric": m,
                "value":  []interface{}{ts, val},
            })
        }
    }
    return all
}

type rangeRes struct {
    Data struct {
        Result []struct {
            Metric map[string]interface{} `json:"metric"`
            Values [][2]interface{}       `json:"values"`
        } `json:"result"`
    } `json:"data"`
}

func fetchWindowsRange(p *ChronoProxy, params url.Values, endpoint, command string) []map[string]interface{} {
    var all []map[string]interface{}
    for i, offset := range p.offsets {
        tf := p.timeframes[i]
        start := parseTime(params.Get("start")) - offset
        end := parseTime(params.Get("end")) - offset
        params.Set("start", strconv.FormatInt(start, 10))
        params.Set("end",   strconv.FormatInt(end,   10))

        u := endpoint + "?" + buildQueryString(params)
        resp, err := p.client.Get(u)
        if err != nil {
            continue
        }
        body, _ := io.ReadAll(resp.Body)
        resp.Body.Close()

        var jr rangeRes
        if err := json.Unmarshal(body, &jr); err != nil {
            continue
        }
        for _, s := range jr.Data.Result {
            shifted := make([]interface{}, len(s.Values))
            for j, pair := range s.Values {
                tsf := pair[0].(float64)
                ts := int64(tsf) + offset
                val := fmt.Sprintf("%v", pair[1])
                shifted[j] = []interface{}{ts, val}
            }
            m := copyMetric(s.Metric)
            m["chrono_timeframe"] = tf
            if command != "" {
                m["_command"] = command
            }
            all = append(all, map[string]interface{}{
                "metric": m,
                "values": shifted,
            })
        }
    }
    return all
}

// ─── HELPERS ───────────────────────────────────────────────────────────────────

// containsString checks if arr contains s
func containsString(arr []interface{}, s string) bool {
    for _, v := range arr {
        if str, ok := v.(string); ok && str == s {
            return true
        }
    }
    return false
}

// parseTime parses int seconds or RFC3339, falling back to now
func parseTime(s string) int64 {
    if i, err := strconv.ParseInt(s, 10, 64); err == nil {
        return i
    }
    if t, err := time.Parse(time.RFC3339, s); err == nil {
        return t.Unix()
    }
    return time.Now().Unix()
}

// signature returns a JSON string of metric without synthetic labels
func signature(m map[string]interface{}) string {
    cp := copyMetric(m)
    delete(cp, "chrono_timeframe")
    delete(cp, "_command")
    keys := make([]string, 0, len(cp))
    for k := range cp {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    ord := map[string]interface{}{}
    for _, k := range keys {
        ord[k] = cp[k]
    }
    b, _ := json.Marshal(ord)
    return string(b)
}

// copyMetric deep copies a metric map
func copyMetric(orig map[string]interface{}) map[string]interface{} {
    dup := make(map[string]interface{}, len(orig))
    for k, v := range orig {
        dup[k] = v
    }
    return dup
}

// dedupeSeries removes duplicate series by signature
func dedupeSeries(all []map[string]interface{}) []map[string]interface{} {
    bySig := make(map[string][]map[string]interface{})
    for _, s := range all {
        sig := signature(s["metric"].(map[string]interface{}))
        bySig[sig] = append(bySig[sig], s)
    }
    var out []map[string]interface{}
    for _, grp := range bySig {
        out = append(out, grp...)
    }
    return out
}

// proxyTimeframes lists the windows
func proxyTimeframes() []string {
    return []string{"current", "7days", "14days", "21days", "28days"}
}

// buildLastMonthAverage computes the averaged series per signature
func buildLastMonthAverage(
    seriesList []map[string]interface{},
    isRange bool,
) []map[string]interface{} {

	if DebugMode {
		log.Println("buildLastMonthAverage")
	}

    n := len(proxyTimeframes()) - 1
    if n < 1 {
        return nil
    }
    groups := make(map[string][]map[string]interface{})
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
    var out []map[string]interface{}
    for sig, grp := range groups {
        sums := make(map[int64]float64)
        for _, s := range grp {
            var pts []interface{}
            if isRange {
                pts = s["values"].([]interface{})
            } else {
                pts = []interface{}{s["value"]}
            }
            for _, iv := range pts {
                pair := iv.([]interface{})
                // robust TS conversion
                var tsF float64
                switch t := pair[0].(type) {
                case float64:
                    tsF = t
                case int64:
                    tsF = float64(t)
                case int:
                    tsF = float64(t)
                case json.Number:
                    if f, err := t.Float64(); err == nil {
                        tsF = f
                    } else {
                        continue
                    }
                default:
                    continue
                }
                minute := (int64(tsF) / 60) * 60
                vStr := fmt.Sprintf("%v", pair[1])
                v, err := strconv.ParseFloat(vStr, 64)
                if err != nil {
                    continue
                }
                sums[minute] += v
            }
        }
        var mins []int64
        for m := range sums {
            mins = append(mins, m)
        }
        sort.Slice(mins, func(i, j int) bool { return mins[i] < mins[j] })
        var ptsOut []interface{}
        for _, m := range mins {
            avg := sums[m] / float64(n)
            ptsOut = append(ptsOut, []interface{}{m, fmt.Sprintf("%g", avg)})
        }
        metric := make(map[string]interface{})
        json.Unmarshal([]byte(sig), &metric)
        metric["chrono_timeframe"] = "lastMonthAverage"
        if isRange {
            out = append(out, map[string]interface{}{"metric": metric, "values": ptsOut})
        } else {
            last := ptsOut[len(ptsOut)-1].([]interface{})
            out = append(out, map[string]interface{}{"metric": metric, "value": last})
        }
    }
	if DebugMode {
		log.Printf("buildLastMonthAverage: %d series", len(out))
	}
    return out
}

// appendCompare adds compareAgainstLast28 for both instant & range
func appendCompare(
    base []map[string]interface{},
    curMap, avgMap map[string]map[string]interface{},
    command string,
    isRange bool,
) []map[string]interface{} {
	if DebugMode {
		log.Println("appendCompare")
	}
	// base is the current series
    out := base

    for sig, c := range curMap {
        a, ok := avgMap[sig]
        if !ok {
            continue
        }

        // prepare metric
        orig := c["metric"].(map[string]interface{})
        nm := copyMetric(orig)
        nm["chrono_timeframe"] = "compareAgainstLast28"
        if command != "" {
            nm["_command"] = command
        }

        if !isRange {
            // instant case
            cv := c["value"].([]interface{})
            av := a["value"].([]interface{})
            vc, _ := strconv.ParseFloat(fmt.Sprintf("%v", cv[1]), 64)
            va, _ := strconv.ParseFloat(fmt.Sprintf("%v", av[1]), 64)
            diff := vc - va
            out = append(out, map[string]interface{}{
                "metric": nm,
                "value":  []interface{}{cv[0], fmt.Sprintf("%g", diff)},
            })
        } else {
            // range case: build lookup of average by timestamp
            aVals := a["values"].([]interface{})
            avgByTs := make(map[int64]float64, len(aVals))
            for _, iv := range aVals {
                pair := iv.([]interface{})
                // robust timestamp decode
                var tsF float64
                switch t := pair[0].(type) {
                case float64:
                    tsF = t
                case int64:
                    tsF = float64(t)
                case int:
                    tsF = float64(t)
                case json.Number:
                    if f, err := t.Float64(); err == nil {
                        tsF = f
                    } else {
                        continue
                    }
                default:
                    continue
                }
                ts := int64(tsF)
                v, _ := strconv.ParseFloat(fmt.Sprintf("%v", pair[1]), 64)
                avgByTs[ts] = v
            }

            // subtract average from current series point-by-point
            cVals := c["values"].([]interface{})
            var valsOut []interface{}
            for _, iv := range cVals {
                pair := iv.([]interface{})
                var tsF float64
                switch t := pair[0].(type) {
                case float64:
                    tsF = t
                case int64:
                    tsF = float64(t)
                case int:
                    tsF = float64(t)
                case json.Number:
                    if f, err := t.Float64(); err == nil {
                        tsF = f
                    } else {
                        continue
                    }
                default:
                    continue
                }
                ts := int64(tsF)
                vc, _ := strconv.ParseFloat(fmt.Sprintf("%v", pair[1]), 64)
                va := avgByTs[ts] // zero if missing
                diff := vc - va
                valsOut = append(valsOut, []interface{}{ts, fmt.Sprintf("%g", diff)})
            }

            out = append(out, map[string]interface{}{
                "metric": nm,
                "values": valsOut,
            })
        }
    }
	if DebugMode {
		log.Printf("appendCompare: %d series", len(out))
	}
    return out
}

// appendPercent adds percentCompareAgainstLast28 for both instant & range
func appendPercent(
    base []map[string]interface{},
    curMap, avgMap map[string]map[string]interface{},
    command string,
    isRange bool,
) []map[string]interface{} {

	if DebugMode {
		log.Println("appendPercent")
	}

    out := base

    for sig, c := range curMap {
        a, ok := avgMap[sig]
        if !ok {
            continue
        }

        orig := c["metric"].(map[string]interface{})
        nm := copyMetric(orig)
        nm["chrono_timeframe"] = "percentCompareAgainstLast28"
        if command != "" {
            nm["_command"] = command
        }

        if !isRange {
            cv := c["value"].([]interface{})
            av := a["value"].([]interface{})
            vc, _ := strconv.ParseFloat(fmt.Sprintf("%v", cv[1]), 64)
            va, _ := strconv.ParseFloat(fmt.Sprintf("%v", av[1]), 64)
            pct := 0.0
            if va != 0 {
                pct = (vc - va) / va * 100
            }
            out = append(out, map[string]interface{}{
                "metric": nm,
                "value":  []interface{}{cv[0], fmt.Sprintf("%g", pct)},
            })
        } else {
            aVals := a["values"].([]interface{})
            avgByTs := make(map[int64]float64, len(aVals))
            for _, iv := range aVals {
                pair := iv.([]interface{})
                var tsF float64
                switch t := pair[0].(type) {
                case float64:
                    tsF = t
                case int64:
                    tsF = float64(t)
                case int:
                    tsF = float64(t)
                case json.Number:
                    if f, err := t.Float64(); err == nil {
                        tsF = f
                    } else {
                        continue
                    }
                default:
                    continue
                }
                ts := int64(tsF)
                v, _ := strconv.ParseFloat(fmt.Sprintf("%v", pair[1]), 64)
                avgByTs[ts] = v
            }

            cVals := c["values"].([]interface{})
            var valsOut []interface{}
            for _, iv := range cVals {
                pair := iv.([]interface{})
                var tsF float64
                switch t := pair[0].(type) {
                case float64:
                    tsF = t
                case int64:
                    tsF = float64(t)
                case int:
                    tsF = float64(t)
                case json.Number:
                    if f, err := t.Float64(); err == nil {
                        tsF = f
                    } else {
                        continue
                    }
                default:
                    continue
                }
                ts := int64(tsF)
                vc, _ := strconv.ParseFloat(fmt.Sprintf("%v", pair[1]), 64)
                va := avgByTs[ts]
                pct := 0.0
                if va != 0 {
                    pct = (vc - va) / va * 100
                }
                valsOut = append(valsOut, []interface{}{ts, fmt.Sprintf("%g", pct)})
            }

            out = append(out, map[string]interface{}{
                "metric": nm,
                "values": valsOut,
            })
        }
    }

	if DebugMode {
		log.Printf("appendPercent: %d series", len(out))
	}

    return out
}


// filterByTimeframe returns only series matching tf label
func filterByTimeframe(
    all []map[string]interface{},
    tf string,
) []map[string]interface{} {
    var out []map[string]interface{}
    for _, s := range all {
        if s["metric"].(map[string]interface{})["chrono_timeframe"] == tf {
            out = append(out, s)
        }
    }
    return out
}

// writeJSON writes a Prometheus-style v1 JSON response
func writeJSON(w http.ResponseWriter, rt string, result []map[string]interface{}) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "status": "success",
        "data": map[string]interface{}{
            "resultType": rt,
            "result":     result,
        },
    })
}

// writeJSONRaw writes arbitrary JSON
func writeJSONRaw(w http.ResponseWriter, v interface{}) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(v)
}

// indexBySignature builds two maps keyed by metric signature:
//   - curMap: all “current” series from `all`
//   - avgMap: all synthetic average series from `avgList`
func indexBySignature(
    all []map[string]interface{},
    avgList []map[string]interface{},
) (map[string]map[string]interface{}, map[string]map[string]interface{}) {

    curMap := make(map[string]map[string]interface{}, len(all))
    avgMap := make(map[string]map[string]interface{}, len(avgList))

    // collect current series
    for _, s := range all {
        m := s["metric"].(map[string]interface{})
        if tf, ok := m["chrono_timeframe"].(string); ok && tf == "current" {
            curMap[signature(m)] = s
        }
    }
    // collect average series
    for _, s := range avgList {
        m := s["metric"].(map[string]interface{})
        avgMap[signature(m)] = s
    }
    return curMap, avgMap
}

// appendWithCommand merges in avgList into base, injecting _command into each
// synthetic series if command is non-empty.
func appendWithCommand(
    base []map[string]interface{},
    avgList []map[string]interface{},
    command string,
) []map[string]interface{} {
    out := base
    for _, a := range avgList {
        if command != "" {
            a["metric"].(map[string]interface{})["_command"] = command
        }
        out = append(out, a)
    }
    return out
}

