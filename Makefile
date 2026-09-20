.PHONY: build release test test-race bench vet fmt fmt-check install clean

VERSION ?= dev
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o diagnos ./cmd/diagnos

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
	install -m 0755 diagnos "$(DESTDIR)$(BINDIR)/diagnos"
	install -d "$(DESTDIR)/etc/vm-native-diagnos"
	install -m 0644 app.yaml "$(DESTDIR)/etc/vm-native-diagnos/app.yaml"

release:
	rm -rf dist
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/vm-native-diagnos_linux_amd64 ./cmd/diagnos
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/vm-native-diagnos_linux_arm64 ./cmd/diagnos
	cp app.yaml dist/app.yaml
	chmod 0755 dist/vm-native-diagnos_linux_*
	cd dist && sha256sum vm-native-diagnos_linux_* app.yaml > checksums.txt

clean:
	rm -rf dist
	rm -f diagnos
