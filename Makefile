.PHONY: build install test test-race lint test-integration release verify

VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || printf unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
SOURCE_DATE_EPOCH ?= $(shell git show -s --format=%ct HEAD 2>/dev/null)
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
RELEASE_DIR ?= dist/$(VERSION)
TARGETS ?=
SIGN_IDENTITY ?=
SIGN_KEYCHAIN ?=
NOTARY_KEY ?=
NOTARY_KEY_ID ?=
NOTARY_ISSUER ?=
LDFLAGS := -s -w \
	-X github.com/KoukeNeko/taiga-cli/internal/version.Version=$(VERSION) \
	-X github.com/KoukeNeko/taiga-cli/internal/version.Commit=$(COMMIT) \
	-X github.com/KoukeNeko/taiga-cli/internal/version.BuildDate=$(BUILD_DATE)

build:
	mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/taiga ./cmd/taiga

install: build
	install -d "$(DESTDIR)$(BINDIR)"
	install -m 0755 bin/taiga "$(DESTDIR)$(BINDIR)/taiga"

test:
	go test ./...

test-race:
	go test -race ./...

lint:
	go vet ./...
	golangci-lint run

test-integration:
	./scripts/test-integration.sh

release:
	@test -n "$(VERSION)" && test "$(VERSION)" != "dev" || (printf '%s\n' 'VERSION must be a semantic release version such as v1.2.3' >&2; exit 2)
	@test -n "$(COMMIT)" && test "$(COMMIT)" != "unknown" || (printf '%s\n' 'COMMIT must identify the release commit' >&2; exit 2)
	@test -n "$(SOURCE_DATE_EPOCH)" || (printf '%s\n' 'SOURCE_DATE_EPOCH is required' >&2; exit 2)
	go run ./cmd/releasepack \
		--version "$(VERSION)" \
		--commit "$(COMMIT)" \
		--source-date-epoch "$(SOURCE_DATE_EPOCH)" \
		--output "$(RELEASE_DIR)" \
		--targets "$(TARGETS)" \
		--sign-identity "$(SIGN_IDENTITY)" \
		--sign-keychain "$(SIGN_KEYCHAIN)" \
		--notary-key "$(NOTARY_KEY)" \
		--notary-key-id "$(NOTARY_KEY_ID)" \
		--notary-issuer "$(NOTARY_ISSUER)"

verify: test test-race lint build
