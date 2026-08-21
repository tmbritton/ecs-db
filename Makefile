.PHONY: build build-headless run generate fmt clean test e2e e2e-seed

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

# End-to-end suite: drives Forge in a real browser against the fixture project
# in e2e/fixtures/project. It builds the binary and reseeds the database itself,
# so this is the whole command.
#
# Forge's characteristic failure is silent — a Datastar attribute naming an
# unregistered plugin renders perfectly and does nothing — and no Go test,
# linter or screenshot can see it. That is what this suite is for.
e2e:
	./scripts/e2e.sh

# Rebuild the fixture database on its own, for poking at it with sqlite3.
e2e-seed:
	go run ./e2e/fixtures/seed

test: generate
	go test ./...
# -race on the packages that have concurrency to get wrong. Without it
# TestSession_IsSafeForConcurrentUse proves nothing: a missing mutex only shows
# up under the detector.
	go test -race ./internal/forge/... ./internal/agent/
# run.go is behind the ebitengine tag, so `go test ./...` never compiles the
# game composition root. Typecheck it so a refactor can't break the tagged
# build with every other signal still green.
	CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	go vet -tags ebitengine ./...
