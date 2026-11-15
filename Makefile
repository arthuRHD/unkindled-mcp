GO ?= go
BINARY := bin/unkindled-mcp
IMAGE ?= unkindled-mcp:dev
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/arthuRHD/unkindled-mcp/internal/mcp.Version=$(VERSION)
PKG := ./cmd/unkindled-mcp

.PHONY: all build install test race vet fmt fmt-check check docker clean

all: check build

build:
	@mkdir -p $(dir $(BINARY))
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

install:
	$(GO) install -trimpath -ldflags "$(LDFLAGS)" $(PKG)

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

fmt-check:
	@unformatted="$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))"; \
	if [ -n "$$unformatted" ]; then \
		echo "These files are not gofmt-clean:"; echo "$$unformatted"; exit 1; \
	fi

check: fmt-check vet test

docker:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .

clean:
	rm -rf bin dist coverage.out coverage.txt
