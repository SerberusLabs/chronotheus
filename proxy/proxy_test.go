package proxy

import (
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestNewChronoProxy(t *testing.T) {
    p := NewChronoProxy()
    
    // Test timeframe setup
    expectedTimeframes := []string{"current", "7days", "14days", "21days", "28days"}
    if len(p.timeframes) != len(expectedTimeframes) {
        t.Errorf("Expected %d timeframes, got %d", len(expectedTimeframes), len(p.timeframes))
    }
    for i, tf := range expectedTimeframes {
        if p.timeframes[i] != tf {
            t.Errorf("Expected timeframe %s, got %s", tf, p.timeframes[i])
        }
    }

    // Test offset calculations
    expectedOffsets := []int64{
        0,
        7 * 24 * 3600,
        14 * 24 * 3600,
        21 * 24 * 3600,
        28 * 24 * 3600,
    }
    if len(p.offsets) != len(expectedOffsets) {
        t.Errorf("Expected %d offsets, got %d", len(expectedOffsets), len(p.offsets))
    }
    for i, offset := range expectedOffsets {
        if p.offsets[i] != offset {
            t.Errorf("Expected offset %d, got %d", offset, p.offsets[i])
        }
    }

    // Test HTTP client initialization
    if p.client == nil {
        t.Error("HTTP client not initialized")
    }
}

func TestServeHTTP(t *testing.T) {
    tests := []struct {
        name           string
        path           string
        method         string
        expectedStatus int
        debugMode      bool
    }{
        {
            name:           "Query endpoint",
            path:           "/api/v1/query",
            method:         "GET",
            expectedStatus: http.StatusOK,
            debugMode:      false,
        },
        {
            name:           "Query range endpoint",
            path:           "/api/v1/query_range",
            method:         "GET",
            expectedStatus: http.StatusOK,
            debugMode:      false,
        },
        {
            name:           "Labels endpoint",
            path:           "/api/v1/labels",
            method:         "GET",
            expectedStatus: http.StatusOK,
            debugMode:      false,
        },
        {
            name:           "Label values endpoint",
            path:           "/api/v1/label/foo/values",
            method:         "GET",
            expectedStatus: http.StatusOK,
            debugMode:      false,
        },
        {
            name:           "Debug mode query",
            path:           "/api/v1/query",
            method:         "GET",
            expectedStatus: http.StatusOK,
            debugMode:      true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Setup
            DebugMode = tt.debugMode
            p := NewChronoProxy()
            
            // Create test request
            req := httptest.NewRequest(tt.method, "http://localhost:8080/prometheus_9090"+tt.path, nil)
            w := httptest.NewRecorder()

            // Execute request
            p.ServeHTTP(w, req)

            // Verify response
            if w.Code != tt.expectedStatus {
                t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
            }
        })
    }
}