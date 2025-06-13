package main

import (
	"fmt"
	"log"
)

/*
ExamplePlugin demonstrates how to create a Chronotheus plugin.

Data Format:
    Input/Output data is a slice of maps representing Prometheus metrics:
    []map[string]interface{} where each map contains:
    {
        "metric": map[string]string{        // Labels map
            "label1": "value1",
            "label2": "value2"
        },
        "value": []interface{}{            // For instant queries (vector)
            float64(timestamp),            // Unix timestamp
            "1234.5"                       // String value
        }
        // OR for range queries (matrix)
        "values": [][]interface{}{         // Array of [timestamp, value] pairs
            {float64(timestamp), "1234.5"},
            {float64(timestamp), "5678.9"}
        }
    }

Plugin Lifecycle:
    1. Plugin is loaded when .so file is dropped into plugins directory
    2. Init() is called immediately after loading
    3. Handle() is called for each query that specifies this plugin
    4. Plugin is unloaded when .so file is removed

Usage in Prometheus Queries:
    my_metric{_plugin="example"}  // Will process through this plugin

Build Command:
    go build -buildmode=plugin -o example.so main.go
*/

// Plugin is the exported plugin instance
var Plugin ExamplePlugin

// ExamplePlugin implements the plugin interface
type ExamplePlugin struct{}

// Init is called when the plugin is first loaded
func (p ExamplePlugin) Init() error {
    log.Printf("🔌 Example Plugin initialized!")
    return nil
}

// GetIdentifier returns the unique name for this plugin
func (p ExamplePlugin) GetIdentifier() string {
    return "example"
}

// Handle processes the metrics data
func (p ExamplePlugin) Handle(data []map[string]interface{}) ([]map[string]interface{}, error) {
    // Process each metric in the dataset
    for _, metric := range data {
        // Access the metric labels
        if labels, ok := metric["metric"].(map[string]string); ok {
            // Add our custom label
            labels["example_plugin"] = "processed"
        }

        // Handle instant query values (vector)
        if val, ok := metric["value"].([]interface{}); ok {
            if len(val) == 2 {
                // val[0] is timestamp (float64)
                // val[1] is value (string)
                if strVal, ok := val[1].(string); ok {
                    // Modify the value if needed
                    metric["value"].([]interface{})[1] = fmt.Sprintf("modified_%s", strVal)
                }
            }
        }

        // Handle range query values (matrix)
        if vals, ok := metric["values"].([][]interface{}); ok {
            for i, pair := range vals {
                if len(pair) == 2 {
                    // pair[0] is timestamp (float64)
                    // pair[1] is value (string)
                    if strVal, ok := pair[1].(string); ok {
                        // Modify each value if needed
                        vals[i][1] = fmt.Sprintf("modified_%s", strVal)
                    }
                }
            }
        }
    }

    return data, nil
}