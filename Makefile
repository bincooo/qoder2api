# qoder2api cross-compilation targets.
# Each OS/arch target is written to bin/<name> and also produce a
# convenience build with the default `make build`.

APP      := qoder2api
BINDIR   := bin
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

GO       := go
LDFLAGS  := -s -w -X main.version=$(VERSION)

.PHONY: all build linux windows darwin test clean list

## Build for the host OS/arch.
build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/$(APP) ./cmd/qoder2api

## Cross-compile helpers (also built by `make all`).
linux:
	@mkdir -p $(BINDIR)
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/linux/amd64/$(APP)  ./cmd/qoder2api
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/linux/arm64/$(APP)  ./cmd/qoder2api

windows:
	@mkdir -p $(BINDIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/windows/amd64/$(APP).exe ./cmd/qoder2api

darwin:
	@mkdir -p $(BINDIR)
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/darwin/amd64/$(APP)   ./cmd/qoder2api
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/darwin/arm64/$(APP)   ./cmd/qoder2api

## Build for all OS/arch targets.
all: linux windows darwin

## Run the test suite.
test:
	$(GO) test ./...

## Remove build artifacts.
clean:
	rm -rf $(BINDIR)

## List all targets.
list:
	@$(MAKE) -pRrq : -f $(firstword $(MAKEFILE_LIST)) 2>/dev/null | \
		awk -F':-? ' '/^[a-zA-Z][a-zA-Z0-9_\/-]*:([^=]|$$)/ {print $$1}' | sort -u