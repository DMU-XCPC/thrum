# Everything Go writes during a build lives under $HOME by default, and the default
# module proxy is not reachable from every network. Both are pinned here instead, so
# that a build behaves the same whatever the surrounding environment looks like.

ROOT       := $(CURDIR)
export GOCACHE    := $(ROOT)/.gocache/build
export GOMODCACHE := $(ROOT)/.gocache/mod
export GOPATH     := $(ROOT)/.gocache/path
export GOPROXY    := https://goproxy.cn,direct
export GOSUMDB    := sum.golang.google.cn

.PHONY: build test vet fmt tidy clean

# The binary lives with the rest of the build state, so that nothing has to be
# installed to try it.
BIN := $(ROOT)/.gocache/ws

build:
	cd tool && go build -o $(BIN) ./cmd/ws
	@echo built $(BIN)

test:
	cd tool && go test ./...

vet:
	cd tool && go vet ./...

fmt:
	cd tool && gofmt -l -w .

tidy:
	cd tool && go mod tidy

clean:
	rm -rf .gocache
