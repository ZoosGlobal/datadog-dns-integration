# Makefile
# Zoos Global — Microsoft DNS Monitor for Datadog
# https://www.zoosglobal.com

BINARY   = dns-monitor.exe
VERSION  = 1.0.0
MODULE   = github.com/ZoosGlobal/datadog-dns-integration
LDFLAGS  = -s -w -X main.version=$(VERSION)

.PHONY: build clean deps lint release

## build: cross-compile for Windows amd64 (runs on any OS)
build:
	@mkdir -p dist
	GOOS=windows GOARCH=amd64 go build \
		-ldflags "$(LDFLAGS)" \
		-o dist/$(BINARY) \
		.
	@echo "Built: dist/$(BINARY)"

## deps: download and tidy Go modules
deps:
	go mod download
	go mod tidy

## lint: run go vet
lint:
	GOOS=windows GOARCH=amd64 go vet ./...

## clean: remove build artifacts
clean:
	rm -rf dist/

## release: build + zip for distribution
release: build
	cd dist && zip dns-monitor-v$(VERSION)-windows-amd64.zip $(BINARY)
	@echo "Release: dist/dns-monitor-v$(VERSION)-windows-amd64.zip"