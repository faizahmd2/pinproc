# pinproc development and packaging entrypoint
.PHONY: build build-provider release build-package-core build-package-provider package-deb package-provider-jev test test-race bench vet fmt fmt-check install clean
VERSION ?= 0.1.0
GOARCH ?= amd64
NFPM_VERSION ?= v2.47.0
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o pinproc ./cmd/diagnos

build-provider:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o pinproc-provider-jev ./cmd/pinproc-provider-jev

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

release:
	rm -rf dist
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/pinproc_linux_amd64 ./cmd/diagnos
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/pinproc_linux_arm64 ./cmd/diagnos
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/pinproc-provider-jev_linux_amd64 ./cmd/pinproc-provider-jev
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/pinproc-provider-jev_linux_arm64 ./cmd/pinproc-provider-jev

build-package-core:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -trimpath -ldflags "$(LDFLAGS)" -o dist/pinproc_linux_$(GOARCH) ./cmd/diagnos

build-package-provider:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -trimpath -ldflags "$(LDFLAGS)" -o dist/pinproc-provider-jev_linux_$(GOARCH) ./cmd/pinproc-provider-jev

package-deb: build-package-core
	GOARCH=$(GOARCH) VERSION=$(VERSION) go run github.com/goreleaser/nfpm/v2/cmd/nfpm@$(NFPM_VERSION) package --config packaging/pinproc.nfpm.yaml --packager deb --target dist/

package-provider-jev: build-package-provider
	GOARCH=$(GOARCH) VERSION=$(VERSION) go run github.com/goreleaser/nfpm/v2/cmd/nfpm@$(NFPM_VERSION) package --config packaging/providers/jev.nfpm.yaml --packager deb --target dist/

install: build
	install -d "$(DESTDIR)/usr/local/bin"
	install -m 0755 pinproc "$(DESTDIR)/usr/local/bin/pinproc"

clean:
	rm -rf dist
	rm -f pinproc pinproc-provider-jev