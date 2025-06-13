package main

import (
	"fmt"
	"log"
	"math"
	"strconv"
)

/*
PredictionPlugin implements future value forecasting for Prometheus metrics.

Features:
- Automatically detects data interval (1m, 5m, etc)
- Uses linear regression for trend analysis
- Maintains the same interval pattern in predictions
- Handles both instant and range queries
- Adds prediction_source="forecast" label to predicted data

Usage in Prometheus:
    # Basic forecast
    rate(node_cpu_seconds_total[5m]){_plugin="prediction"}

    # Multiple metrics forecast
    {__name__=~"node_.*", _plugin="prediction"}

Build:
    go build -buildmode=plugin -o ..\prediction.so main.go
*/

var Plugin PredictionPlugin

type PredictionPlugin struct{}

func (p PredictionPlugin) Init() error {
    log.Printf("Prediction Plugin initialised - Ready to peek into the future!")
    return nil
}

func (p PredictionPlugin) GetIdentifier() string {
    return "prediction"
}

func (p PredictionPlugin) Handle(data []map[string]interface{}) ([]map[string]interface{}, error) {
    result := make([]map[string]interface{}, 0, len(data)*2) // Pre-allocate for efficiency

    for _, metric := range data {
        // Keep original data
        result = append(result, metric)

        // Create prediction metrics
        predicted, err := p.predictMetric(metric)
        if err != nil {
            log.Printf("Warning: Failed to predict metric: %v", err)
            continue
        }
        result = append(result, predicted)
    }

    return result, nil
}

func (p PredictionPlugin) predictMetric(metric map[string]interface{}) (map[string]interface{}, error) {
    prediction := make(map[string]interface{})

    // Copy metric labels
    if metricLabels, ok := metric["metric"].(map[string]string); ok {
        newLabels := make(map[string]string, len(metricLabels)+1)
        for k, v := range metricLabels {
            newLabels[k] = v
        }
        newLabels["prediction_source"] = "forecast"
        prediction["metric"] = newLabels
    }

    // Handle different query types
    if values, ok := metric["values"].([][]interface{}); ok {
        // Range query (matrix)
        return p.handleRangeQuery(prediction, values)
    } else if value, ok := metric["value"].([]interface{}); ok {
        // Instant query (vector)
        return p.handleInstantQuery(prediction, value)
    }

    return nil, fmt.Errorf("unsupported metric format")
}

func (p PredictionPlugin) handleRangeQuery(prediction map[string]interface{}, values [][]interface{}) (map[string]interface{}, error) {
    if len(values) < 2 {
        return nil, fmt.Errorf("insufficient data points for prediction")
    }

    // Extract timestamps and values
    timestamps := make([]float64, len(values))
    datapoints := make([]float64, len(values))
    for i, pair := range values {
        timestamps[i] = pair[0].(float64)
        if val, err := strconv.ParseFloat(pair[1].(string), 64); err == nil {
            datapoints[i] = val
        }
    }

    // Calculate interval between data points
    interval := timestamps[1] - timestamps[0]
    
    // Calculate future timestamps
    lastTimestamp := timestamps[len(timestamps)-1]
    futurePoints := len(timestamps)
    futureValues := make([][]interface{}, futurePoints)

    // Perform linear regression
    slope, intercept := linearRegression(timestamps, datapoints)

    // Generate predictions
    for i := 0; i < futurePoints; i++ {
        futureTimestamp := lastTimestamp + (interval * float64(i+1))
        predictedValue := slope*futureTimestamp + intercept
        
        // Add some variance based on historical volatility
        volatility := calculateVolatility(datapoints)
        adjustedValue := addRandomVariance(predictedValue, volatility)
        
        futureValues[i] = []interface{}{
            futureTimestamp,
            fmt.Sprintf("%.4f", adjustedValue),
        }
    }

    prediction["values"] = futureValues
    return prediction, nil
}

func (p PredictionPlugin) handleInstantQuery(prediction map[string]interface{}, value []interface{}) (map[string]interface{}, error) {
    if len(value) != 2 {
        return nil, fmt.Errorf("invalid instant query format")
    }

    timestamp := value[0].(float64)
    currentVal, err := strconv.ParseFloat(value[1].(string), 64)
    if err != nil {
        return nil, err
    }

    // For instant queries, project one step into the future
    futureTimestamp := timestamp + 60 // Default to 1-minute projection
    predictedValue := currentVal * 1.1 // Simple 10% increase prediction

    prediction["value"] = []interface{}{
        futureTimestamp,
        fmt.Sprintf("%.4f", predictedValue),
    }

    return prediction, nil
}

// linearRegression calculates the slope and intercept for prediction
func linearRegression(x, y []float64) (float64, float64) {
    n := float64(len(x))
    sumX, sumY := 0.0, 0.0
    sumXY, sumXX := 0.0, 0.0

    for i := 0; i < len(x); i++ {
        sumX += x[i]
        sumY += y[i]
        sumXY += x[i] * y[i]
        sumXX += x[i] * x[i]
    }

    slope := (n*sumXY - sumX*sumY) / (n*sumXX - sumX*sumX)
    intercept := (sumY - slope*sumX) / n

    return slope, intercept
}

// calculateVolatility estimates data volatility
func calculateVolatility(values []float64) float64 {
    if len(values) < 2 {
        return 0.1 // Default volatility
    }

    sum := 0.0
    mean := 0.0
    for _, v := range values {
        mean += v
    }
    mean /= float64(len(values))

    for _, v := range values {
        diff := v - mean
        sum += diff * diff
    }

    return math.Sqrt(sum / float64(len(values)-1))
}

// addRandomVariance adds controlled randomness to predictions
func addRandomVariance(value, volatility float64) float64 {
    // Use volatility to add some randomness while maintaining trend
    variance := (math.Sin(value) + 1) * volatility * 0.1
    return value + variance
}