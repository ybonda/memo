.PHONY: build install test clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  = -s -w \
  -X github.com/ybonda/memo/internal/version.Version=$(VERSION) \
  -X github.com/ybonda/memo/internal/version.Commit=$(COMMIT) \
  -X github.com/ybonda/memo/internal/version.Date=$(DATE)

build:
	@mkdir -p bin
	CGO_ENABLED=1 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/memo .

install:
	CGO_ENABLED=1 go install -trimpath -ldflags "$(LDFLAGS)" .

test:
	CGO_ENABLED=1 go test ./...

clean:
	rm -rf bin
