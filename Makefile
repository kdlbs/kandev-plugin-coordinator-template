.DEFAULT_GOAL := build

.PHONY: build run test test-backend test-recipes typecheck-recipes audit-recipes sdk-check \
	fmt vet package package-host verify-package verify-package-host clean

# When you rename the plugin, update BIN and VERSION to match manifest.yaml's
# id and version (PKG_OUT is derived from them).
BIN := bin/kandev-plugin-coordinator-template
VERSION := 0.2.1
STAGE := .build/stage
PKG_OUT := kandev-plugin-coordinator-template-$(VERSION).tar.gz

# Packaging-only Kandev source input. SDK compilation uses public modules.
# Checkout the exact commit in .kandev-tooling-ref with apps/backend and pluginsdk.
# Run plugin-pack in its own module so backend dependencies stay out of go.sum.
KANDEV_TOOLING ?= ../kandev-tooling/apps/backend

sdk-check:
	go run github.com/kdlbs/kandev/pluginsdk/cmd/kandev-sdk-check@v0.1.0 --manifest manifest.yaml --frontend-sdk node_modules/@kdlbs/plugin-sdk

## Build the plugin binary for the host platform (development use). kandev
## itself always installs from `make package`/`package-host` output, not this.
build: sdk-check
	mkdir -p bin
	go build -o $(BIN) ./server/...

## Build + run. Mainly for -race / manual smoke checks: kandev normally spawns
## this binary itself via the go-plugin handshake, so a manually-started
## process has nothing to talk to on the other end.
run: build
	./$(BIN)

test: sdk-check test-backend typecheck-recipes test-recipes

test-backend:
	go test ./server/... ./recipes/source-control/server/...

test-recipes:
	npm run test:recipes

typecheck-recipes:
	npm run typecheck:recipes

audit-recipes:
	npm audit --audit-level=high

fmt:
	gofmt -l .

vet:
	go vet ./server/... ./recipes/source-control/server/...

## Cross-compile server/plugin-<goos>-<goarch>[.exe] for every platform in
## manifest.yaml's runtime.executables, stage manifest.yaml + ui/ alongside
## them, and pack the tree into $(PKG_OUT) with
## backend-owned cmd/plugin-pack from the separately pinned tooling checkout.
## Install the tarball via Settings > Plugins or curl -F package=@...
package: sdk-check
	rm -rf $(STAGE)
	mkdir -p $(STAGE)/server
	cp manifest.yaml $(STAGE)/manifest.yaml
	cp -r ui $(STAGE)/ui
	GOOS=linux   GOARCH=amd64 go build -o $(STAGE)/server/plugin-linux-amd64       ./server
	GOOS=linux   GOARCH=arm64 go build -o $(STAGE)/server/plugin-linux-arm64       ./server
	GOOS=darwin  GOARCH=amd64 go build -o $(STAGE)/server/plugin-darwin-amd64      ./server
	GOOS=darwin  GOARCH=arm64 go build -o $(STAGE)/server/plugin-darwin-arm64      ./server
	GOOS=windows GOARCH=amd64 go build -o $(STAGE)/server/plugin-windows-amd64.exe ./server
	cd $(KANDEV_TOOLING) && go run ./cmd/plugin-pack -dir $(CURDIR)/$(STAGE) -out $(CURDIR)/$(PKG_OUT)
	rm -rf $(STAGE)
	@echo "Wrote $(PKG_OUT)"

## Package for the host platform only — faster local iteration than the full
## 5-platform `make package` (matches plugin-pack's -platform-only).
package-host: sdk-check
	rm -rf $(STAGE)
	mkdir -p $(STAGE)/server
	cp manifest.yaml $(STAGE)/manifest.yaml
	cp -r ui $(STAGE)/ui
	go build -o $(STAGE)/server/plugin-$$(go env GOOS)-$$(go env GOARCH)$$(go env GOEXE) ./server
	cd $(KANDEV_TOOLING) && go run ./cmd/plugin-pack -dir $(CURDIR)/$(STAGE) -out $(CURDIR)/$(PKG_OUT) -platform-only
	rm -rf $(STAGE)
	@echo "Wrote $(PKG_OUT)"

## Build and validate the all-platform archive: plugin-pack checks the manifest;
## this additionally verifies checksums, expected binaries, and that opt-in
## recipe/development files did not leak into the generated starter package.
verify-package: package
	@tmp="$$(mktemp -d)"; trap 'rm -rf "$$tmp"' EXIT; \
		tar -xzf "$(PKG_OUT)" -C "$$tmp"; \
		test -f "$$tmp/manifest.yaml"; \
		test -f "$$tmp/ui/bundle.js"; \
		test -f "$$tmp/checksums.txt"; \
		for executable in \
			plugin-linux-amd64 plugin-linux-arm64 \
			plugin-darwin-amd64 plugin-darwin-arm64 \
			plugin-windows-amd64.exe; do \
			test -f "$$tmp/server/$$executable"; \
		done; \
		test ! -e "$$tmp/recipes"; \
		test ! -e "$$tmp/package.json"; \
		if command -v sha256sum >/dev/null 2>&1; then \
			(cd "$$tmp" && sha256sum -c checksums.txt); \
		else \
			(cd "$$tmp" && shasum -a 256 -c checksums.txt); \
		fi

## Faster equivalent for local/CI host-platform packaging.
verify-package-host: package-host
	@tmp="$$(mktemp -d)"; trap 'rm -rf "$$tmp"' EXIT; \
		tar -xzf "$(PKG_OUT)" -C "$$tmp"; \
		host_executable="plugin-$$(go env GOOS)-$$(go env GOARCH)$$(go env GOEXE)"; \
		test -f "$$tmp/manifest.yaml"; \
		test -f "$$tmp/ui/bundle.js"; \
		test -f "$$tmp/checksums.txt"; \
		test -f "$$tmp/server/$$host_executable"; \
		test ! -e "$$tmp/recipes"; \
		test ! -e "$$tmp/package.json"; \
		if command -v sha256sum >/dev/null 2>&1; then \
			(cd "$$tmp" && sha256sum -c checksums.txt); \
		else \
			(cd "$$tmp" && shasum -a 256 -c checksums.txt); \
		fi

clean:
	rm -rf bin $(STAGE) kandev-plugin-coordinator-template-*.tar.gz
