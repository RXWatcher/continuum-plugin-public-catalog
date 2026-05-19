BINARY := continuum-plugin-public-catalog
GO ?= go
NPM ?= npm

.PHONY: build test clean

node_modules/.package-lock.json: package.json package-lock.json
	$(NPM) ci

build: node_modules/.package-lock.json
	$(NPM) run build
	$(GO) build -o $(BINARY) ./cmd/continuum-plugin-public-catalog

test: node_modules/.package-lock.json
	$(NPM) run build
	$(GO) test ./...

clean:
	rm -f $(BINARY)
