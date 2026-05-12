.PHONY: build test install uninstall all
.DEFAULT_GOAL := all

all: test install

build:
	go build -o git-remove-path-history .

test:
	go test ./...

install:
	go install .

uninstall:
	rm -f $(shell go env GOPATH)/bin/git-remove-path-history
