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

// _________ .__                                __  .__
// \_   ___ \|  |_________  ____   ____   _____/  |_|  |__   ____  __ __  ______
// /    \  \/|  |  \_  __ \/  _ \ /    \ /  _ \   __\  |  \_/ __ \|  |  \/  ___/
// \     \___|   Y  \  | \(  <_> )   |  (  <_> )  | |   Y  \  ___/|  |  /\___ \
//  \______  /___|  /__|   \____/|___|  /\____/|__| |___|  /\___  >____//____  >
//         \/     \/                  \/                 \/     \/           \/
// Prometheus metrics proxy - Andy Dixon <andy@andydixon.com> github.com/andydixon

// Welcome to Chronotheus!
// Our time-travelling metrics adventure starts here!
//
// This is Mission Control - where we:
// 1. Set up our debug systems
// 2. Launch our proxy
// 3. Start listening for incoming metrics
//
// Think of it as Houston launching a space mission,
// but instead of rockets, we're launching a proxy that can
// peek through time at your metrics!

package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/andydixon/chronotheus/internal/plugin"
	"github.com/andydixon/chronotheus/proxy"
)

// Version information - these will be set at build time
var (
	Version   = "dev"
	CommitSHA = "unknown"
	BuildTime = "unknown"
)

// Global plugin manager instance
var GlobalPluginManager *plugin.Manager

// main is our entrypoint
//
// 1. Check if we're in debug mode (like checking instruments)
// 2. Configure our logging systems (like setting up comms)
// 3. Fire up our time-traveling proxy (like igniting engines)
// 4. Start listening for requests (like "We have liftoff!")
//
// If anything goes wrong during launch, we'll let you know
// exactly what happened and why.
//
// Pro tip: Run with -debug flag for verbose logging:
//   ./chronotheus -debug
func main() {
	debug := flag.Bool("debug", false, "enable debug logging")
	listen := flag.String("listen", "0.0.0.0:8080", "address to listen on (ip:port)")

	flag.Parse()

	fmt.Println("-={[ C h r o n e t h e u s ]}=-");
	fmt.Printf("Version: %s\nGit Commit: %s\nBuild Time: %s\n", Version, CommitSHA, BuildTime)
	
	if *debug {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		log.Println("Debug logging enabled")
	}

	proxy.DebugMode = *debug

	pluginPath := "./plugins"
	GlobalPluginManager = plugin.NewManager(pluginPath)
	
	if err := plugin.WatchPlugins(GlobalPluginManager); err != nil {
		log.Printf("Failed to initialize plugin watcher: %v", err)
	}

	p := proxy.NewChronoProxy()
	log.Printf("🚀 Chronotheus v%s (commit %s) launching!\n", Version, CommitSHA)
	log.Printf("👂 Listening on %s", *listen)
	if err := http.ListenAndServe(*listen, p); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
