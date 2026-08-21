.PHONY: build build-headless run generate fmt clean test

BREW_PREFIX := /home/linuxbrew/.linuxbrew
XORGPROTO   := $(shell brew --prefix xorgproto 2>/dev/null || echo $(BREW_PREFIX)/Cellar/xorgproto/2025.1)

CGO_CFLAGS  := -I$(BREW_PREFIX)/include -I$(XORGPROTO)/include
CGO_LDFLAGS := -L$(BREW_PREFIX)/lib

generate:
	go run github.com/a-h/templ/cmd/templ generate

build: generate
	CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	go build -tags ebitengine -o bin/ecs-db ./cmd/ecs-db

# Proves the Forge/CLI path builds with no graphics toolchain at all.
build-headless: generate
	CGO_ENABLED=0 go build -o bin/ecs-db-headless ./cmd/ecs-db

run: build
	./bin/ecs-db run

fmt:
	gofumpt -w .
	go run github.com/a-h/templ/cmd/templ fmt .

clean:
	rm -rf bin/
	find ./internal ./cmd -name '*_templ.go' -delete

test: generate
	go test ./...
# run.go is behind the ebitengine tag, so `go test ./...` never compiles the
# game composition root. Typecheck it so a refactor can't break the tagged
# build with every other signal still green.
	CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	go vet -tags ebitengine ./...
