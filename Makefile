# Everything Go writes during a build lives under $HOME by default, and the default
# module proxy is not reachable from every network. Both are pinned here instead, so
# that a build behaves the same whatever the surrounding environment looks like.

ROOT       := $(CURDIR)
export GOCACHE    := $(ROOT)/.gocache/build
export GOMODCACHE := $(ROOT)/.gocache/mod
export GOPATH     := $(ROOT)/.gocache/path
export GOPROXY    := https://goproxy.cn,direct
export GOSUMDB    := sum.golang.google.cn

.PHONY: build test test-harness vet fmt tidy clean

# The binary lives with the rest of the build state, so that nothing has to be
# installed to try it.
BIN := $(ROOT)/.gocache/thrum

build:
	cd tool && go build -o $(BIN) ./cmd/thrum
	@echo built $(BIN)

# -count=1 rather than the test cache, and not for freshness in general: the e2e package
# imports none of the tool's packages — it builds ./cmd/thrum and drives the binary — so
# go's cache key cannot see the code it exercises, and a cached pass there is a pass for
# a binary that no longer exists. Every other package caches correctly.
test:
	cd tool && go test -count=1 ./...
	@$(MAKE) --no-print-directory test-harness

# The harness plugin is a different language and a different runtime, and building the
# format must not require either — so it is skipped where node is absent rather than
# failing, and run from `test` so that it cannot rot unnoticed.
test-harness:
	@if command -v node >/dev/null 2>&1; then cd harness/dsh && node --test test/; \
	else echo "no node: skipping the harness plugin's tests"; fi

vet:
	cd tool && go vet ./...

fmt:
	cd tool && gofmt -l -w .

tidy:
	cd tool && go mod tidy

clean:
	rm -rf .gocache
