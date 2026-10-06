# pinproc development and packaging entrypoint
.PHONY: build release build-package-core package-deb test test-race bench vet fmt fmt-check install clean
VERSION ?= 0.1.0
GOARCH ?= amd64
NFPM_VERSION ?= v2.47.0
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o pinproc ./cmd/diagnos

test:
	go test ./...

bench:
	go test -bench=. -benchmem ./internal/engine/... ./internal/procfs/... ./internal/pressure/...

test-race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	find . -name '*.go' -not -path './.git/*' -print0 | xargs -0 gofmt -w

fmt-check:
	@test -z "$$(find . -name '*.go' -not -path './.git/*' -print0 | xargs -0 gofmt -l)"

release:
	rm -rf dist
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/pinproc_linux_amd64 ./cmd/diagnos
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/pinproc_linux_arm64 ./cmd/diagnos

build-package-core:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -trimpath -ldflags "$(LDFLAGS)" -o dist/pinproc_linux_$(GOARCH) ./cmd/diagnos

package-deb: build-package-core
	PACKAGE_ARCH=$(GOARCH) VERSION=$(VERSION) go run github.com/goreleaser/nfpm/v2/cmd/nfpm@$(NFPM_VERSION) package --config packaging/pinproc.nfpm.yaml --packager deb --target dist/

install: build
	install -d "$(DESTDIR)/usr/local/bin"
	install -m 0755 pinproc "$(DESTDIR)/usr/local/bin/pinproc"

clean:
	rm -rf dist
	rm -f pinproc
