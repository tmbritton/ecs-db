.PHONY: build run clean

# Build the CLI application
build:
	go build -o bin/ecsdb ./cmd/cli

# Run the CLI application
run: build
	./bin/ecsdb

# Run without rebuilding (useful when you rebuild manually)
run-only:
	./bin/ecsdb

# Clean build artifacts
clean:
	rm -rf bin/

# Build and run in one command
dev: build run-only

# Run tests
test:
	go test ./...
