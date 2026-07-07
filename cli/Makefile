BINARY := ./bin/ctxpm
GENERATED := ./internal/engine/zz_bundled_docs_generated.go
GO := $(shell if [ -x /usr/local/go/bin/go ]; then printf /usr/local/go/bin/go; elif [ -x /opt/homebrew/bin/go ]; then printf /opt/homebrew/bin/go; elif command -v go >/dev/null 2>&1; then command -v go; else printf go; fi)

.PHONY: generate build test clean

generate:
	cd ./internal/engine && $(GO) generate ./...

build:
	mkdir -p ./bin
	$(MAKE) generate
	$(GO) build -o $(BINARY) .

test:
	$(MAKE) generate
	$(GO) test ./...

clean:
	rm -f $(BINARY) $(GENERATED)
