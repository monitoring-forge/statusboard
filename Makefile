GIT_VER := $(shell git describe --tags)
GITCOMMIT?=$(shell git describe --dirty --always)
LDFLAGS=-ldflags "-w -s -X main.version=${VERSION} -X main.commit=${GITCOMMIT}"

all: statusboard

.PHONY: statusboard

statusboard: logs.go toml.go worker.go handlers.go main.go files/index.html
	go build $(LDFLAGS) -o statusboard

linux: logs.go toml.go worker.go handlers.go main.go files/index.html
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o statusboard

check:
	go test -v
	CGO_ENABLED=1 go test -race