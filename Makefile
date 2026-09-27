.PHONY: build release package test test-race bench vet fmt fmt-check install clean

VERSION ?= dev
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o pinproc ./cmd/diagnos

test:
	go test ./...

bench:
	go test -bench=. -benchmem ./internal/engine/... ./internal/procfs/...

test-race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	find . -name '*.go' -not -path './.git/*' -print0 | xargs -0 gofmt -w

fmt-check:
	@test -z "$$(find . -name '*.go' -not -path './.git/*' -print0 | xargs -0 gofmt -l)"

install: build
	install -d "$(DESTDIR)$(BINDIR)"
	install -m 0755 pinproc "$(DESTDIR)$(BINDIR)/pinproc"

package:
	@test -n "$(GOARCH)" || (echo "GOARCH is required" && exit 1)
	@test -n "$(VERSION)" || (echo "VERSION is required" && exit 1)
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -trimpath -ldflags "$(LDFLAGS)" -o dist/pinproc_linux_$(GOARCH) ./cmd/diagnos
	NFPM_BINARY="$(CURDIR)/dist/pinproc_linux_$(GOARCH)" VERSION="$(VERSION)" GOARCH="$(GOARCH)" go run github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.47.0 pkg --config packaging/nfpm.yaml --packager deb --target "dist/pinproc_$(VERSION)_$(GOARCH).deb"

release:
	rm -rf dist
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/pinproc_linux_amd64 ./cmd/diagnos
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/pinproc_linux_arm64 ./cmd/diagnos
	cp config.example.yaml dist/config.example.yaml
	chmod 0755 dist/pinproc_linux_*
	cd dist && sha256sum pinproc_linux_* config.example.yaml > checksums.txt

clean:
	rm -rf dist
	rm -f pinproc
