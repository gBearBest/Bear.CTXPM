BINARY := ./bin/ctxpm

.PHONY: build test clean

build:
	mkdir -p ./bin
	go build -o $(BINARY) .

test:
	go test ./...

clean:
	rm -f $(BINARY)
