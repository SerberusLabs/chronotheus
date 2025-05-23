#!/bin/zsh

# Get the git information
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT_SHA=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(date -u '+%Y-%m-%d %H:%M:%S')

# Build with version information
go build -ldflags "-s -w -X main.Version=${VERSION} -X main.CommitSHA=${COMMIT_SHA} -X 'main.BuildTime=${BUILD_TIME}'" -o chronotheus
