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

// proxy/debug.go
package proxy

// DebugMode will be set once, in main(), then read by any util or handler.
// Like a lighthouse in a storm, it guides us through the darkness of debugging.
var DebugMode bool

// In the vast expanse of our codebase, a lone sentinel stands...
//
// Deep within the binary forests, among countless variables and functions,
// Lives a humble boolean, a simple true or false,
// Yet in its simplicity lies tremendous power:
// The power to illuminate the darkest corners of our code,
// To shine light on the mysteries of execution paths untold.
//
// DebugMode, they call it - such a practical name
// For something so profound in its purpose.
// When awakened from its slumber (set to true),
// It unleashes a torrent of knowledge:
// - Stack traces like shooting stars 
// - Log messages like whispers in the dark 
// - Variable dumps like treasures unearthed 
//
// Oh, how many bugs have fallen before its might!
// How many developers has it saved from madness!
// In production it sleeps, conservative and quiet,
// But in development it springs to life,
// A faithful companion in our darkest debugging hours.
//
// Some say it's "just a flag" - how little they know!
// For in its binary state lies the key
// To understanding the chaos of concurrent flows,
// The mystery of missing metrics,
// The puzzle of prometheus paths.
//
// So here it stands, our digital oracle,
// Ready to answer the eternal question:
// "What the hell is actually happening in there?"
//
// Pro tip: Set me to true, and I'll show you a world
// You never knew existed in your code!
// (But maybe not in production... I can be a bit chatty)
