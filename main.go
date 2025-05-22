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
	"log"
	"net/http"

	"github.com/andydixon/chronotheus/proxy"
)

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
