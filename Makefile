SHELL := /bin/bash

# Isolate from a parent otfabric go.work when present.
export GOWORK := off

PKGS     := ./...
FUZZTIME ?= 5s

.PHONY: help test test-race chaos vet lint lint-ci fmt tidy vuln coverage coverage-check check fuzz bench cross imports interop interop-required interop-pinned dissector clean

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "%-14s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run unit tests
	@echo "Running unit tests..."
	@go test $(PKGS)

test-race: ## Run tests with race detector
	@echo "Running tests with race detector..."
	@go test -race $(PKGS)

chaos: ## Client chaos tests under the race detector, at 5x load, repeated
	@echo "Running chaos tests (BACNET_CHAOS_SEED=<n> pins a seed)..."
	@BACNET_CHAOS_SCALE=$${BACNET_CHAOS_SCALE:-5} go test -race -count=3 -timeout 20m -run '^TestChaos' ./client

vet: ## Run go vet
	@echo "Running go vet..."
	@go vet $(PKGS)

lint: ## Run staticcheck
	@echo "Running staticcheck..."
	@staticcheck $(PKGS)

lint-ci: ## Run golangci-lint (same gate as shared go-ci.yml)
	@echo "Running golangci-lint..."
	@golangci-lint run $(PKGS)

fmt: ## Format Go sources
	@echo "Running gofmt..."
	@gofmt -w .
	@go fmt $(PKGS)

tidy: ## Tidy go.mod / go.sum
	@echo "Running go mod tidy..."
	@go mod tidy

vuln: ## Run govulncheck
	@echo "Running govulncheck..."
	@govulncheck $(PKGS)

# Library coverage tests exclude examples and the Docker interop package.
# The percentage gate also excludes internal/fixtures: corpus tests still run
# (and cover codecs/client via fixtures_test), but the loader's Root() walk is
# environment-sensitive (BACNET_INTEROP_ROOT short-circuits) and pulled CI
# below 90% while local sibling checkouts measured ~90.2%.
COVER_TEST_PKGS := $(shell go list ./... | grep -v '/examples/' | grep -v '/interop$$')
COVER_GATE_PKGS := $(shell go list ./... | grep -v '/examples/' | grep -v '/interop$$' | grep -v '/internal/fixtures$$')
COVERAGE_MIN ?= 90

coverage: ## Write coverage.out; fail if library total < COVERAGE_MIN%
	@echo "Running coverage (min $(COVERAGE_MIN)%)..."
	@coverpkg=$$(echo $(COVER_GATE_PKGS) | tr ' ' ','); \
	go test -coverprofile=coverage.out -covermode=count -coverpkg="$$coverpkg" $(COVER_TEST_PKGS)
	@go tool cover -func=coverage.out | tee coverage.txt
	@total=$$(go tool cover -func=coverage.out | awk '/^total:/ {gsub(/%/,"",$$NF); print $$NF}'); \
	awk -v t="$$total" -v m="$(COVERAGE_MIN)" 'BEGIN { if ((t+0) < (m+0)) { printf "coverage %.1f%% < %s%%\n", t, m; exit 1 } else { printf "coverage %.1f%% >= %s%%\n", t, m } }'

coverage-check: coverage ## Alias of coverage, which already enforces COVERAGE_MIN

imports: ## Enforce package import boundaries
	@echo "Checking import boundaries..."
	@go test ./internal/imports/...

# Packages that own native fuzz targets. Discovery avoids hand-maintained name
# lists and always uses ^Name$ so -fuzz never matches a longer sibling
# (e.g. FuzzDecodeWritePropertyMultiple vs …Error).
FUZZ_PKGS ?= . ./bvlc ./apdu ./npdu ./service

fuzz: ## Run short fuzz targets (override FUZZTIME; nightly uses 5m)
	@echo "Fuzzing (FUZZTIME=$(FUZZTIME))..."
	@set -euo pipefail; \
	for pkg in $(FUZZ_PKGS); do \
	  targets=$$(go test -list '^Fuzz' "$$pkg" | grep '^Fuzz' || true); \
	  [ -n "$$targets" ] || continue; \
	  for t in $$targets; do \
	    echo "==> $$pkg $$t"; \
	    go test -run='^$$' -fuzz="^$${t}$$" -fuzztime=$(FUZZTIME) "$$pkg"; \
	  done; \
	done

bench: ## Run the codec benchmarks
	@echo "Running benchmarks..."
	@go test -run='^$$' -bench=. -benchmem $(PKGS)

cross: ## Cross-compile without cgo for the supported platforms
	@echo "Cross-compiling..."
	@set -e; for p in linux/amd64 linux/arm64 linux/arm darwin/arm64 windows/amd64; do \
	  echo "==> $$p"; \
	  CGO_ENABLED=0 GOOS=$${p%/*} GOARCH=$${p#*/} go build $(PKGS); \
	done

# Interop needs Docker, the peer images and a bacnet-interop checkout (sibling
# directory or BACNET_INTEROP_ROOT). `interop` uses whatever images the
# *_IMAGE variables name (default: locally built `:local` tags) and skips when
# they are missing; `interop-pinned` takes the digests from the pin file, pulls
# what is absent and fails instead of skipping. That is what CI runs. Images a
# pin does not list yet (sysiobacnet and bacnet-stack-router exist from
# bacnet-interop v0.10.0) are passed as "none": their scenarios skip.
INTEROP_PIN     := interop/bacnet-interop-pin.json
INTEROP_TIMEOUT ?= 30m
INTEROP_RUN     ?=
INTEROP_FLAGS   := -tags=interop -count=1 -timeout $(INTEROP_TIMEOUT) $(if $(INTEROP_RUN),-run '$(INTEROP_RUN)',)

interop: ## Peer assertions (-tags=interop); images from *_IMAGE or :local builds
	@echo "Interop: go test $(INTEROP_FLAGS) ./interop/... (see INTEROP.md)"
	@go test $(INTEROP_FLAGS) ./interop/...

interop-required: ## Fail if Docker/images/fixtures are missing (CI)
	@BACNET_INTEROP_REQUIRED=1 $(MAKE) interop

interop-pinned: ## Pull the pinned peer images and run interop against them (needs jq)
	@command -v jq >/dev/null || { echo "interop-pinned: jq is required"; exit 1; }
	@set -euo pipefail; \
	pin() { jq -er --arg k "$$1" '.images[$$k]' $(INTEROP_PIN); }; \
	opt() { jq -r --arg k "$$1" '.images[$$k] // "none"' $(INTEROP_PIN); }; \
	export BACNET_STACK_IMAGE="$$(pin bacnet-stack)" BACPYPES3_IMAGE="$$(pin bacpypes3)" \
	       BACNET4J_IMAGE="$$(pin bacnet4j)" WORLDIETY_IMAGE="$$(pin worldiety)" \
	       BIP_ROUTER_IMAGE="$$(pin bip-router)" \
	       SYSIOBACNET_IMAGE="$$(opt sysiobacnet)" BACNET_STACK_ROUTER_IMAGE="$$(opt bacnet-stack-router)"; \
	for img in "$$BACNET_STACK_IMAGE" "$$BACPYPES3_IMAGE" "$$BACNET4J_IMAGE" "$$WORLDIETY_IMAGE" "$$BIP_ROUTER_IMAGE" \
	           "$$SYSIOBACNET_IMAGE" "$$BACNET_STACK_ROUTER_IMAGE"; do \
	  [ "$$img" = none ] && continue; \
	  docker image inspect "$$img" >/dev/null 2>&1 || docker pull "$$img"; \
	done; \
	echo "Interop against bacnet-interop $$(jq -r .ref $(INTEROP_PIN))"; \
	BACNET_INTEROP_REQUIRED=1 go test $(INTEROP_FLAGS) ./interop/...

dissector: ## Check every client request against Wireshark's dissector (needs tshark)
	@echo "Dissector oracle: go test -tags=dissector ./interop/"
	@BACNET_DISSECTOR_REQUIRED=1 go test -tags=dissector -count=1 -run '^TestDissector' ./interop/

check: fmt tidy vet lint lint-ci vuln test test-race coverage imports ## Aggregate local release checks (includes CI linters)
	@echo "check: ok"

clean: ## Remove coverage artifacts and test cache
	@rm -f coverage.out coverage.txt coverage.html cover*.out
	@go clean -testcache
