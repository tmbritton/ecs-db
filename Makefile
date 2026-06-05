.PHONY: build run clean test

BREW_PREFIX := /home/linuxbrew/.linuxbrew
XORGPROTO   := $(shell brew --prefix xorgproto 2>/dev/null || echo $(BREW_PREFIX)/Cellar/xorgproto/2025.1)

CGO_CFLAGS  := -I$(BREW_PREFIX)/include -I$(XORGPROTO)/include
CGO_LDFLAGS := -L$(BREW_PREFIX)/lib

build:
	CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	go build -tags ebitengine -o bin/game ./cmd/game

run: build
	./bin/game

clean:
	rm -rf bin/

test:
	go test ./...
