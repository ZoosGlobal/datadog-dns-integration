# Makefile
# Zoos Global — Microsoft DNS Monitor for Datadog
# https://www.zoosglobal.com

BINARY       = dns-monitor.exe
VERSION     ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0-dev")
VERSION_BARE = $(subst v,,$(VERSION))
LDFLAGS      = -s -w -X main.version=$(VERSION_BARE)
ASSET        = datadog-dns-integration-$(VERSION)-windows-amd64

.PHONY: build deps vet clean package tag help

## help: show available targets
help:
	@echo ""
	@echo "  Zoos Global — Microsoft DNS Monitor for Datadog"
	@echo ""
	@grep -E '^## ' Makefile | sed 's/## /  /'
	@echo ""

## deps: download and tidy Go modules
deps:
	go mod download
	go mod tidy

## vet: run go vet for windows/amd64 target
vet:
	GOOS=windows GOARCH=amd64 go vet ./...

## build: cross-compile dns-monitor.exe for windows/amd64
build: deps vet
	@mkdir -p dist
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build \
		-ldflags "$(LDFLAGS)" \
		-o dist/$(BINARY) \
		./...
	@echo "Built: dist/$(BINARY) ($(shell ls -lh dist/$(BINARY) 2>/dev/null | awk '{print $$5}' || echo 'check dist/'))"

## package: build + zip everything a customer needs for deployment
package: build
	@mkdir -p release/$(ASSET)/checks.d
	@mkdir -p release/$(ASSET)/conf.d/dns_monitor.d
	@mkdir -p release/$(ASSET)/scripts
	cp dist/$(BINARY)                       release/$(ASSET)/
	cp dns-monitor-config.yaml.example      release/$(ASSET)/
	cp checks.d/dns_monitor.py              release/$(ASSET)/checks.d/
	cp conf.d/dns_monitor.d/conf.yaml       release/$(ASSET)/conf.d/dns_monitor.d/
	cp scripts/setup.ps1                    release/$(ASSET)/scripts/
	cp README.md                            release/$(ASSET)/
	cp CHANGELOG.md                         release/$(ASSET)/
	cd release && zip -r $(ASSET).zip $(ASSET)/
	cd release && sha256sum $(ASSET).zip > $(ASSET).zip.sha256
	@echo ""
	@echo "  Package : release/$(ASSET).zip"
	@echo "  SHA256  : $$(cat release/$(ASSET).zip.sha256)"

## tag: create and push a release tag — triggers GitHub Actions release workflow
##      usage: make tag VERSION=v1.0.1
tag:
	@if [ -z "$(VERSION)" ] || [ "$(VERSION)" = "v0.0.0-dev" ]; then \
		echo "Usage: make tag VERSION=v1.0.1"; exit 1; \
	fi
	git tag -a $(VERSION) -m "Release $(VERSION)"
	git push origin $(VERSION)
	@echo "Tagged and pushed: $(VERSION)"
	@echo "GitHub Actions will build and publish the release automatically."
	@echo "Watch: https://github.com/ZoosGlobal/datadog-dns-integration/actions"

## clean: remove build artifacts
clean:
	rm -rf dist/ release/