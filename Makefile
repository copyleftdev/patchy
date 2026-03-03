BINARY   := patchy
CMD      := ./cmd/patchy
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE     ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  := -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

.PHONY: build clean test lint vet tidy doctor manifest

build:
	go build $(LDFLAGS) -o bin/$(BINARY) $(CMD)

clean:
	rm -rf bin/

test:
	go test -v -race -count=1 ./...

lint: vet
	@echo "lint passed"

vet:
	go vet ./...

tidy:
	go mod tidy

doctor: build
	./bin/$(BINARY) --doctor

manifest: build
	./bin/$(BINARY) --manifest
