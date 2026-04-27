# sugaar — developer makefile.
# Targets are grouped: build • test • profile • codegen • run • deploy • tls.
# Run `make help` to print this list.

SHELL          := bash
BIN            ?= sugaar
PKG            := ./...
EXAMPLE        ?= ./examples/agent-stream
LDFLAGS        ?= -s -w -X main.version=$(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
GOFLAGS        ?=
DOCKER_IMAGE   ?= sugaar:dev
PROTO_DIR      ?= proto
PROTO_OUT      ?= internal/pb
AVRO_DIR       ?= avro
AVRO_OUT       ?= internal/avrogen
CERT_DIR       ?= ./certs/dev
HOST           ?= localhost

.DEFAULT_GOAL := help

## ---- meta -----------------------------------------------------------------

help:           ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nsugaar targets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

## ---- build ----------------------------------------------------------------

build:          ## Compile every package (sanity build)
	go build $(GOFLAGS) $(PKG)

bin:            ## Build the example agent-stream binary into ./dist
	mkdir -p dist
	go build -ldflags='$(LDFLAGS)' -o dist/$(BIN) $(EXAMPLE)

bin-linux:      ## Cross-compile a linux/amd64 binary for VPS deploy
	mkdir -p dist
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
		go build -ldflags='$(LDFLAGS)' -o dist/$(BIN)-linux-amd64 $(EXAMPLE)

clean:          ## Remove build artifacts and coverage files
	rm -rf dist coverage.out cpu.prof mem.prof trace.out

## ---- test -----------------------------------------------------------------

test:           ## Run unit tests with race detector
	go test -race $(PKG)

test-short:     ## Skip slow tests (e.g. network-bound)
	go test -short -race $(PKG)

cover:          ## Coverage summary
	go test -race -coverprofile=coverage.out $(PKG)
	@go tool cover -func=coverage.out | tail -1

cover-html:     ## Open coverage in browser
	go test -race -coverprofile=coverage.out $(PKG)
	go tool cover -html=coverage.out

bench:          ## Run benchmarks
	go test -bench=. -benchmem -run=^$$ $(PKG)

update-golden:  ## Regenerate golden files (review the diff afterwards!)
	go test $(PKG) -update

vet:            ## Static checks
	go vet $(PKG)

fmt:            ## gofmt on the tree
	gofmt -s -w .

lint:           ## golangci-lint (install: brew install golangci-lint)
	@command -v golangci-lint >/dev/null || { echo "golangci-lint missing — see https://golangci-lint.run"; exit 1; }
	golangci-lint run

check: fmt vet test ## Quick pre-push gate: fmt, vet, race tests

## ---- run ------------------------------------------------------------------

run:            ## Run the example with debug logging
	go run $(EXAMPLE)

dev:            ## Live-reload (requires `air`: go install github.com/air-verse/air@latest)
	@command -v air >/dev/null || { echo "air missing — go install github.com/air-verse/air@latest"; exit 1; }
	air -c .air.toml

watch-test:     ## Re-run tests on file change (requires entr)
	@command -v entr >/dev/null || { echo "entr missing — install with brew/apt"; exit 1; }
	@find . -name '*.go' | entr -c make test-short

## ---- profile --------------------------------------------------------------

profile:        ## Capture and open a 30s CPU profile from a running server
	curl -s -o cpu.prof http://$(HOST):8080/debug/pprof/profile?seconds=30
	go tool pprof -http=:0 cpu.prof

heap:           ## Capture and open a heap profile
	curl -s -o mem.prof http://$(HOST):8080/debug/pprof/heap
	go tool pprof -http=:0 mem.prof

trace:          ## Capture and open a 5s execution trace
	curl -s -o trace.out 'http://$(HOST):8080/debug/pprof/trace?seconds=5'
	go tool trace trace.out

## ---- codegen --------------------------------------------------------------

proto:          ## Generate Go code from .proto files (requires protoc + go plugins)
	@command -v protoc >/dev/null || { echo "protoc missing — brew install protobuf"; exit 1; }
	@command -v protoc-gen-go >/dev/null || \
		go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@command -v protoc-gen-go-grpc >/dev/null || \
		go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	mkdir -p $(PROTO_OUT)
	protoc -I=$(PROTO_DIR) \
		--go_out=$(PROTO_OUT)      --go_opt=paths=source_relative \
		--go-grpc_out=$(PROTO_OUT) --go-grpc_opt=paths=source_relative \
		$(PROTO_DIR)/*.proto

avro:           ## Generate Go structs from .avsc Avro schemas
	@command -v avrogen >/dev/null || \
		go install github.com/hamba/avro/v2/cmd/avrogen@latest
	mkdir -p $(AVRO_OUT)
	@for f in $(AVRO_DIR)/*.avsc; do \
		avrogen -pkg avrogen -o $(AVRO_OUT)/$$(basename $${f%.avsc}).go $$f ; \
	done

gen: proto avro  ## Run all code generators

## ---- tls ------------------------------------------------------------------

tls-dev:        ## Mint a self-signed dev cert for HOST=$(HOST)
	mkdir -p $(CERT_DIR)
	openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
		-keyout $(CERT_DIR)/key.pem -out $(CERT_DIR)/cert.pem \
		-subj "/CN=$(HOST)" -addext "subjectAltName=DNS:$(HOST),IP:127.0.0.1"
	@echo "wrote $(CERT_DIR)/{cert,key}.pem"

## ---- deploy ---------------------------------------------------------------

docker:         ## Build a tiny scratch image of the example
	docker build -t $(DOCKER_IMAGE) -f Dockerfile .

systemd-unit:   ## Print a sample systemd unit — pipe to /etc/systemd/system/<name>.service
	@printf '%s\n' \
	  '[Unit]' \
	  'Description=$(BIN)' \
	  'After=network.target' \
	  '' \
	  '[Service]' \
	  'ExecStart=/usr/local/bin/$(BIN)' \
	  'Restart=on-failure' \
	  'AmbientCapabilities=CAP_NET_BIND_SERVICE' \
	  'User=$(BIN)' \
	  '' \
	  '[Install]' \
	  'WantedBy=multi-user.target'

deploy:         ## scp the linux binary to a VPS via $$VPS_HOST (e.g. user@host)
	@test -n "$$VPS_HOST" || { echo "set VPS_HOST=user@host"; exit 1; }
	scp dist/$(BIN)-linux-amd64 $$VPS_HOST:/usr/local/bin/$(BIN)
	ssh $$VPS_HOST 'sudo systemctl restart $(BIN)'

.PHONY: help build bin bin-linux clean test test-short cover cover-html bench \
        update-golden vet fmt lint check run dev watch-test profile heap trace \
        proto avro gen tls-dev docker systemd-unit deploy
