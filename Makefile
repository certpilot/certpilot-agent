.PHONY: build test test-race lint tidy image verify-profiles clean help

GO  := go
BIN := bin

AGENT_BIN := $(BIN)/certpilot-agent

# ── Build ────────────────────────────────────────────────
build:
	$(GO) build -o $(AGENT_BIN) ./cmd/

# ── Test ─────────────────────────────────────────────────
test:
	$(GO) test ./... -cover

test-race:
	$(GO) test ./... -race

lint:
	$(GO) vet ./...
	@gofmt -l . | grep . && echo "gofmt needed on the files above" && exit 1 || true
	@## staticcheck when it is installed, because it catches a class go vet does
	@## not: dead assignments, impossible conditions, and code nothing reaches.
	@## Not a hard requirement, so a clone can be linted without installing it.
	@##   go install honnef.co/go/tools/cmd/staticcheck@latest
	@if command -v staticcheck >/dev/null 2>&1; then \
		staticcheck ./...; \
	else \
		echo "staticcheck not installed; skipping (go install honnef.co/go/tools/cmd/staticcheck@latest)"; \
	fi

tidy:
	$(GO) mod tidy

## Prove every platform profile against the real service. Starts each one in a
## container, installs a certificate through this agent's own installer, and
## completes a TLS handshake to check the service is serving it after its own
## reload — so a profile in the catalogue is a thing somebody ran, not a thing
## somebody wrote down.
##
## Needs a container runtime. Nothing else in this Makefile does, which is why
## this is its own target and not part of `make test`.
##
## IIS is the one profile this cannot cover: it does not run in a container, so
## it is verified on a Windows runner by the `iis` job in CI instead.
##
##   make verify-profiles              all of them
##   make verify-profiles PROFILE=nginx
verify-profiles:
	./scripts/verify-profiles.sh $(PROFILE)

image:
	docker build -t certpilot-agent .

clean:
	rm -rf $(BIN)/
	rm -f coverage.out

help:
	@echo "CertPilot agent"
	@echo ""
	@echo "  make build             Build the agent into $(AGENT_BIN)"
	@echo "  make test              Run the tests"
	@echo "  make test-race         Run them under the race detector"
	@echo "  make lint              go vet, gofmt and staticcheck"
	@echo "  make verify-profiles   Install to every platform, in containers"
	@echo "  make image             Build the container image"
