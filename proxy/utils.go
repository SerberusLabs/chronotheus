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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ─── PARAMS & STRIPPING ─────────────────────────────────────────────────────────

// parseClientParams is our request detective!
// It digs through both GET and POST params to find everything we need.
//
// It handles:
// - GET parameters (the easy ones in the URL)
// - POST form data (old school but reliable)
// - JSON bodies (fancy modern stuff)
//
// Returns everything in one nice url.Values package.
// Pro tip: This is why you can send requests however you want!
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

    // detectSelectors is our ninja label finder! 
    // When labels aren't in match[], this function searches for them inside the query.
    // It's like finding Easter eggs in your code! 
    //
    // For example, it can find:
    // - chrono_timeframe="7days" in your{labels="here",chrono_timeframe="7days"}
    // - _command="DONT_REMOVE_UNUSED_HISTORICS" hiding in complex queries
    //
    // Returns whatever it finds, empty strings if nothing found.
    // Pro tip: This is why your timeframes work even in complex queries!
    func detectSelectors(vals url.Values) (string, string) {
        tf, cmd := "", ""
        query := vals.Get("query")
    
        // Detect chrono_timeframe in inline labels
        if re := regexp.MustCompile(`chrono_timeframe="([^"]+)"`); re.MatchString(query) {
            if matches := re.FindStringSubmatch(query); len(matches) > 1 {
                tf = matches[1]
                if DebugMode {
                    log.Printf("[DEBUG] Found inline timeframe: %s", tf)
                }
            }
        }
    
        // Detect _command in inline labels
        if re := regexp.MustCompile(`_command="([^"]+)"`); re.MatchString(query) {
            if matches := re.FindStringSubmatch(query); len(matches) > 1 {
                cmd = matches[1]
                if DebugMode {
                    log.Printf("[DEBUG] Found inline command: %s", cmd)
                }
            }
        }
    
        return tf, cmd
    }

    // stripLabelFromParam is our label eraser! 
    // It removes specific labels from Prometheus queries so they don't confuse the upstream Prometheus server.
//
// For example, it turns:
//   metric{label="value",chrono_timeframe="7days"} 
// Into:
//   metric{label="value"}
//
// It's like those people who clean up after a parade - nobody sees them work,
// but everything would be a mess without them!
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

    // remapMatch is our traffic 'acktchuuuuallly' equivalent!
    // It makes sure we use match[] instead of match because Prometheus 
    // gets grumpy if we don't. (Yes, the [] matters. A lot.) - #squareBracketLivesMatter
    //
    // Think of it like those signs that say "Please use other door" -
// it just helps everyone go the right way!
func remapMatch(vals url.Values) {
        if m := vals["match"]; len(m) > 0 && vals.Get("match[]") == "" {
            vals["match[]"] = m
            delete(vals, "match")
        }
    }

    // ─── FORWARD / BUILD QS ───────────────────────────────────────────────────────

    // buildQueryString is our URL builder! 
    // Takes all our parameters and builds a proper query string.
    //
    // The tricky part: It handles both single values AND arrays:
    //   single: ?param=value
    //   array:  ?param[]=value1&param[]=value2
    //
    // Pro tip: This is why your URLs always work, even with complex queries!
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

    // forward is our proxy bouncer! 
    // It takes requests and sends them to Prometheus exactly as they came,
    // except for the URL which points to our upstream server.
    //
    // It's like a mail forwarding service - takes your mail and sends it on,
    // keeping all the original packaging intact!
    //
    // Pro tip: This is how we handle all the requests we don't need to modify
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
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        resp, err := client.Do(req)
        if err != nil {
            http.Error(w, err.Error(), http.StatusBadGateway)
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

    // fetchWindowsInstant is our time-traveling data fetcher! Wibbly wobbly, timey wimey stuff!
    // For each timeframe (current/7days/14days/etc), it:
    // 1. Adjusts the timestamp backwards by the offset
    // 2. Fetches data from Prometheus
    // 3. Shifts timestamps back to present time
    // 4. Adds chrono_timeframe labels
    //
    // It's like having multiple parallel universes of data,
    // each showing what happened at different points in time!
//
// Pro tip: This is what makes comparing data across time possible!
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

    // fetchWindowsRange is like fetchWindowsInstant's big brother!
    // Instead of single points, it fetches entire ranges of data.
    // Perfect for when you need to plot graphs or analyse trends.
    //
    // For each timeframe, it:
    // 1. Adjusts both start and end times
    // 2. Fetches all the data points
    // 3. Shifts everything back to present time
    // 4. Labels everything properly
func fetchWindowsRange(p *ChronoProxy, params url.Values, endpoint, command string) []map[string]interface{} {
        var all []map[string]interface{}
        for i, offset := range p.offsets {
            
            if DebugMode {
                log.Printf("fetchWindowsRange: %d offset %d", i, offset)
            }

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

            if DebugMode {
                log.Printf("fetchWindowsRange offset- Got Data: %s", u)
            }

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

            if DebugMode {
                log.Printf("fetchWindowsRange offset loop timeshifted")
            }   

        }
        if DebugMode {
            log.Printf("fetchWindowsRange offset loop completed: ", len(all))
        }
        return all
    }

    // ─── HELPERS ───────────────────────────────────────────────────────────────────

    // containsString is our needle-in-haystack finder!
    // Simple but crucial - it checks if a string is in an array.
    // Because sometimes you just need to know if something's there!
    func containsString(arr []interface{}, s string) bool {
        for _, v := range arr {
            if str, ok := v.(string); ok && str == s {
                return true
            }
        }
        return false
    }

    // parseTime is our time wizard!
    // Give it:
    // - Unix timestamps (like "1621234567")
    // - RFC3339 strings (like "2023-05-22T12:34:56Z")
    // - Nothing (it'll use current time)
    //
    // And it always gives you back Unix seconds!
    // No more time format headaches! 🎉
    func parseTime(s string) int64 {
        if i, err := strconv.ParseInt(s, 10, 64); err == nil {
            return i
        }
        if t, err := time.Parse(time.RFC3339, s); err == nil {
            return t.Unix()
        }
        return time.Now().Unix()
    }

    // signature is our metric fingerprinter!
    // It takes a metric and creates a unique JSON string that identifies it,
    // ignoring our special labels (chrono_timeframe and _command).
    //
    // Think of it like a fingerprint for your metrics - 
    // same metric = same signature, even if the timestamps are different!
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

    // copyMetric is our metric photocopier!
    // Makes an exact copy of a metric map because sometimes
    // you need to modify it without changing the original.
    //
    // Pro tip: Go maps are reference types - this prevents accidents!
    func copyMetric(orig map[string]interface{}) map[string]interface{} {
        dup := make(map[string]interface{}, len(orig))
        for k, v := range orig {
            dup[k] = v
        }
        return dup
    }

    // dedupeSeries is our duplicate destroyer! We should not need this, but it is here for safety. 
    // That's my excuse anyways. I need to make sure we don't have duplicates in our series at any time, it's a memory waste.
    // Takes a bunch of series and combines any that have the same signature.
    // Because nobody likes seeing the same thing twice!
//
// Think of it like cleaning up after a party - 
// making sure there's only one of each cup left on the table.
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

    // proxyTimeframes is our time window menu! This needs to be configurable in the future.
    // It lists all the timeframes we support for our metrics. We should share the data and 
    // have it as a key value pair thing so the second offset is combined with it.
    // Lists all the raw timeframes we support:
    // - current (right now!)
    // - 7days (last week)
    // - 14days (two weeks ago)
    // - 21days (three weeks back)
    // - 28days (a whole month!)
    //
    // Pro tip: These are the building blocks for all our fancy calculations!
    func proxyTimeframes() []string {
        return []string{"current", "7days", "14days", "21days", "28days"}
    }

    // buildLastMonthAverage is our mathmagician! KwikMafs!
    // Takes all your metrics and calculates their average over the last month.
    // It's like finding the "usual" value for everything!
//
// For example:
// - If traffic is usually 1000 req/s
// - But now it's 1500 req/s
// - You know something's up!
//
// Pro tip: This powers our trend detection and comparisons!
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

    // appendCompare is our difference detector!
    // Shows how current values differ from the monthly average.
    // Perfect for spotting when things are different from normal!
//
// For example:
// - Average is 100
// - Current is 150
// - Shows +50 (we're above normal!)
//
// Pro tip: Great for capacity planning and anomaly detection!
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

    // appendPercent is our percentage pal! More KwikMafs!
    // Like appendCompare but shows differences as percentages.
    // Because sometimes "50% higher" means more than "500 more"!
//
// For example:
// - Average is 100
// - Current is 150
// - Shows +50% (we're up by half!)
//
// Pro tip: Perfect for relative comparisons across different scales!
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


    // filterByTimeframe is our series selector! 
    // Only keeps series matching the timeframe you want.
    // If your name isn't on the list, you aint coming in.
    // Removes all series that don't match the given timeframe requested in chrono_timeframe to reduce traffic.
//
// Pro tip: This is why you only see the data you asked for!
func filterByTimeframe(
        all []map[string]interface{},
        tf string,
    ) []map[string]interface{} {
        var out []map[string]interface{}
        if DebugMode {
            log.Printf("Filtering metrics - only returning '%s'", tf)
        }
        for _, s := range all {
            if DebugMode {
                log.Printf("Checking: '%s' matches '%s'",s["metric"].(map[string]interface{})["chrono_timeframe"], tf)
            }
            if s["metric"].(map[string]interface{})["chrono_timeframe"] == tf {
                out = append(out, s)
                if DebugMode {
                    log.Printf("Matched: '%s' matches '%s'",s["metric"].(map[string]interface{})["chrono_timeframe"], tf)
                }
            }
        }
        return out
    }

    // writeJSON is our Prometheus whisperer! 
    // Writes data back in exactly the format Prometheus expects.
    // Because speaking the right language is important, and it has been an absolute pain in the arse at times.
//
// Pro tip: This is why Grafana can read our responses!
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

    // writeJSONRaw is our simple JSON writer! 
    // When you just need to send some JSON and don't care about 
    // the Prometheus format. Quick and dirty!
    func writeJSONRaw(w http.ResponseWriter, v interface{}) {
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(v)
    }

    // indexBySignature is our metric organiser!
    // Takes all your metrics and sorts them into two piles:
    // - Current values (what's happening now)
    // - Average values (what usually happens)
    // It makes it much easier to find things later!
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

    // appendWithCommand is our label injector!
    // Adds command labels to synthetic series when needed.
    // Because sometimes you need to mark where data came from!
//
// Pro tip: This is how we track which series were generated vs raw!
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

// instantRes helps us decode Prometheus instant query responses.
// It's like a template for the JSON that Prometheus sends back!
type instantRes struct {
    Data struct {
        Result []struct {
            Metric map[string]interface{} `json:"metric"`
            Value  [2]interface{}         `json:"value"`
        } `json:"result"`
    } `json:"data"`
}

