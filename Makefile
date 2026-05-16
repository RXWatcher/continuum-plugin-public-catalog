BINARY := continuum-plugin-public-catalog
GO ?= go

.PHONY: build test clean

build:
	$(GO) build -o $(BINARY) ./cmd/continuum-plugin-public-catalog

test:
	$(GO) test ./...

clean:
	rm -f $(BINARY)
