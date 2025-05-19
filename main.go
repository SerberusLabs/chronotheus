package main

import (
    "log"
    "net/http"
    "flag"
    "github.com/andydixon/chronotheus/proxy"
)

func main() {
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()
	if *debug {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		log.Println("Debug logging enabled")
	}

	proxy.DebugMode = *debug

    p := proxy.NewChronoProxy()
    log.Println("🚀 Chronotheus proxy listening on :8080")
    if err := http.ListenAndServe(":8080", p); err != nil {
        log.Fatalf("Server failed: %v", err)
    }
}
