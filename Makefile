.PHONY: test coverage coverage-html coverage-func clean build examples

# Run all tests
test:
	go test -v ./pkg/...

# Run tests with coverage
coverage:
	go test -cover ./pkg/...

# Generate coverage profile and display percentage
coverage-report:
	go test -coverprofile=coverage.out ./pkg/...
	go tool cover -func=coverage.out | grep total

# Generate HTML coverage report
coverage-html:
	go test -coverprofile=coverage.out ./pkg/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Show detailed function-by-function coverage
coverage-func:
	go test -coverprofile=coverage.out ./pkg/...
	go tool cover -func=coverage.out

# Clean build artifacts and coverage files
clean:
	rm -f coverage.out coverage.html
	go clean ./...

# Build all examples
examples:
	@echo "Building examples..."
	cd examples/http-echo && go build -o ../../bin/http-echo
	cd examples/http-echo-instrumented && go build -o ../../bin/http-echo-instrumented
	cd examples/relay-node && go build -o ../../bin/relay-node
	cd examples/relay-initiator && go build -o ../../bin/relay-initiator
	@echo "Examples built in bin/"

# Build Linux binaries for container deployment
examples-linux:
	@echo "Building Linux binaries for container deployment..."
	cd examples/relay-node && GOOS=linux GOARCH=arm64 go build -o relay-node-linux
	cd examples/relay-initiator && GOOS=linux GOARCH=arm64 go build -o relay-initiator-linux
	@echo "Linux binaries built"

# Build everything
build:
	go build ./...
