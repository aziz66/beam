.PHONY: build dev clean docker release test agent

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS = -ldflags "-s -w -X main.version=$(VERSION)"

# Development
dev:
	go run .

# Build for current platform
build:
	go build $(LDFLAGS) -o beam .

# Build CLI
cli:
	go build $(LDFLAGS) -o beam-cli ./cli/

# Build for all platforms
release:
	mkdir -p dist
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/beam-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/beam-linux-arm64 .
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/beam-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/beam-darwin-arm64 .
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/beam-windows-amd64.exe .

# Docker
docker:
	docker build -t beam:$(VERSION) .

# Clean
clean:
	rm -rf dist/ data/ beam beam.exe beam-cli beam-cli.exe

# Run tests
test:
	go test ./... -v -race

# Vet
vet:
	go vet ./...

# Update SRI hashes after editing any JS file
sri:
	bash scripts/update-sri.sh

# Agent (Tauri)
agent:
	cd agent && cargo tauri build
