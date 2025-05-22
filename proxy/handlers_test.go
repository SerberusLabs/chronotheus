package proxy

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"
)

func TestHandleQuery(t *testing.T) {
    tests := []struct {
        name           string
        query          string
        timeframe     string
        command       string
        expectedCode   int
        expectedCount  int
        debugMode      bool
    }{
        {
            name:          "Basic query no timeframe",
            query:         "test_metric",
            timeframe:     "",
            command:       "",
            expectedCode:  http.StatusOK,
            expectedCount: 8, // 5 windows + avg + compare + percent
            debugMode:     false,
        },
        {
            name:          "Query with specific timeframe",
            query:         "test_metric",
            timeframe:     "7days",
            command:       "",
            expectedCode:  http.StatusOK,
            expectedCount: 1,
            debugMode:     false,
        },
        {
            name:          "Query with DONT_REMOVE_UNUSED_HISTORICS",
            query:         "test_metric",
            timeframe:     "",
            command:       "DONT_REMOVE_UNUSED_HISTORICS",
            expectedCode:  http.StatusOK,
            expectedCount: 5, // all windows, no synthetics
            debugMode:     true,
        },
        {
            name:          "Query with lastMonthAverage",
            query:         "test_metric",
            timeframe:     "lastMonthAverage",
            command:       "",
            expectedCode:  http.StatusOK,
            expectedCount: 1,
            debugMode:     false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            DebugMode = tt.debugMode
            p := NewChronoProxy()

            // Build request
            req := httptest.NewRequest("GET", "/api/v1/query", nil)
            q := req.URL.Query()
            q.Add("query", tt.query)
            if tt.timeframe != "" {
                q.Add("query", `{chrono_timeframe="`+tt.timeframe+`"}`)
            }
            if tt.command != "" {
                q.Add("query", `{_command="`+tt.command+`"}`)
            }
            req.URL.RawQuery = q.Encode()

            // Execute request
            w := httptest.NewRecorder()
            p.handleQuery(w, req, "http://localhost:9090", "/api/v1/query")

            // Verify response
            if w.Code != tt.expectedCode {
                t.Errorf("Expected status %d, got %d", tt.expectedCode, w.Code)
            }

            // Parse response
            var resp struct {
                Data struct {
                    Result []map[string]interface{} `json:"result"`
                } `json:"data"`
            }
            if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
                t.Fatalf("Failed to decode response: %v", err)
            }

            // Verify result count
            if len(resp.Data.Result) != tt.expectedCount {
                t.Errorf("Expected %d results, got %d", tt.expectedCount, len(resp.Data.Result))
            }
        })
    }
}

func TestHandleQueryRange(t *testing.T) {
    tests := []struct {
        name          string
        query         string
        timeframe    string
        start        string
        end          string
        step         string
        expectedCode int
    }{
        {
            name:       "Basic range query",
            query:      "test_metric",
            start:      time.Now().Add(-1*time.Hour).Format(time.RFC3339),
            end:        time.Now().Format(time.RFC3339),
            step:       "60",
            expectedCode: http.StatusOK,
        },
        {
            name:       "Range query with timeframe",
            query:      "test_metric",
            timeframe: "7days",
            start:      time.Now().Add(-1*time.Hour).Format(time.RFC3339),
            end:        time.Now().Format(time.RFC3339),
            step:       "60",
            expectedCode: http.StatusOK,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            p := NewChronoProxy()

            // Build request
            req := httptest.NewRequest("GET", "/api/v1/query_range", nil)
            q := req.URL.Query()
            q.Add("query", tt.query)
            q.Add("start", tt.start)
            q.Add("end", tt.end)
            q.Add("step", tt.step)
            if tt.timeframe != "" {
                q.Add("query", `{chrono_timeframe="`+tt.timeframe+`"}`)
            }
            req.URL.RawQuery = q.Encode()

            // Execute request
            w := httptest.NewRecorder()
            p.handleQueryRange(w, req, "http://localhost:9090", "/api/v1/query_range")

            // Verify response
            if w.Code != tt.expectedCode {
                t.Errorf("Expected status %d, got %d", tt.expectedCode, w.Code)
            }
        })
    }
}