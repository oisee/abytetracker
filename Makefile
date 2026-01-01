.PHONY: build run clean all darwin linux windows

BINARY=abytetracker
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Default target
all: build

# Build for current platform
build:
	go build -o $(BINARY) ./cmd/tracker

# Run with demo song
run: build
	./$(BINARY) songs/bossabeat.abt

# Cross-compile for all platforms
release: darwin-amd64 darwin-arm64 linux-amd64 linux-arm64 windows-amd64
	@echo "All builds complete in dist/"

darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build -o dist/$(BINARY)-darwin-amd64 ./cmd/tracker

darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build -o dist/$(BINARY)-darwin-arm64 ./cmd/tracker

linux-amd64:
	GOOS=linux GOARCH=amd64 go build -o dist/$(BINARY)-linux-amd64 ./cmd/tracker

linux-arm64:
	GOOS=linux GOARCH=arm64 go build -o dist/$(BINARY)-linux-arm64 ./cmd/tracker

windows-amd64:
	GOOS=windows GOARCH=amd64 go build -o dist/$(BINARY)-windows-amd64.exe ./cmd/tracker

# Shortcuts
darwin: darwin-arm64
linux: linux-amd64
windows: windows-amd64

clean:
	rm -f $(BINARY)
	rm -rf dist/
	rm -rf _export/
