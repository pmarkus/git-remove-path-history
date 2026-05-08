.PHONY: build test

build:
	go build -o git-remove-path-history .

test:
	go test ./...
