# Everything Go writes during a build lives under $HOME by default, and the default
# module proxy is not reachable from every network. Both are pinned here instead, so
# that a build behaves the same whatever the surrounding environment looks like.

ROOT       := $(CURDIR)
export GOCACHE    := $(ROOT)/.gocache/build
export GOMODCACHE := $(ROOT)/.gocache/mod
export GOPATH     := $(ROOT)/.gocache/path
export GOPROXY    := https://goproxy.cn,direct
export GOSUMDB    := sum.golang.google.cn

.PHONY: build install test test-harness vet fmt tidy clean

# The binary lives with the rest of the build state, so that nothing has to be
# installed to try it.
BIN := $(ROOT)/.gocache/thrum

# CGO_ENABLED=0 is the tooling's rule rather than a preference (tooling.md: one statically linked binary
# per platform, droppable onto a machine that has nothing but git. Without it, a host that
# has a C toolchain links the binary against its own libc, and the result does not run on a
# musl machine at all — the loader says "no such file or directory", which reads like a
# missing file rather than a wrong one.
build:
	cd tool && CGO_ENABLED=0 go build -o $(BIN) ./cmd/thrum
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

# Where it goes, and nowhere else: a machine that runs the tooling should not need a Go
# toolchain to have it, which is what the static build above is for.
PREFIX  ?= /usr/local
DESTDIR ?=

install: build
	install -d $(DESTDIR)$(PREFIX)/bin
	install -m 0755 $(BIN) $(DESTDIR)$(PREFIX)/bin/thrum
	@echo installed $(DESTDIR)$(PREFIX)/bin/thrum

vet:
	cd tool && go vet ./...

fmt:
	cd tool && gofmt -l -w .

tidy:
	cd tool && go mod tidy

clean:
	rm -rf .gocache
