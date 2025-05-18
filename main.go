package main

import (
    "log"
    "net/http"

    "github.com/andydixon/chronotheus/proxy"
)

func main() {
    p := proxy.NewChronoProxy()
    log.Println("🚀 Chronotheus proxy listening on :8080")
    if err := http.ListenAndServe(":8080", p); err != nil {
        log.Fatalf("Server failed: %v", err)
    }
}
