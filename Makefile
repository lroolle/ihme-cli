BINARY_NAME=ihme
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(GIT_COMMIT)"

.PHONY: build
build:
	go build -o $(BINARY_NAME) $(LDFLAGS) ./cmd/ihme

.PHONY: clean
clean:
	go clean
	rm -f $(BINARY_NAME)
	rm -f *.out

.PHONY: test
test:
	go test ./...

.PHONY: test-cover
test-cover:
	go test ./... -coverprofile=cover.out
	go tool cover -func=cover.out | tail -1

.PHONY: vet
vet:
	go vet ./...

.PHONY: install
install: build
	@if [ -z "$(GOPATH)" ]; then \
		echo "GOPATH not set, installing to ~/go/bin"; \
		mkdir -p ~/go/bin; \
		cp $(BINARY_NAME) ~/go/bin/$(BINARY_NAME); \
	else \
		cp $(BINARY_NAME) $(GOPATH)/bin/$(BINARY_NAME); \
	fi
	@echo "Installed as '$(BINARY_NAME)'"

.PHONY: cross
cross:
	GOOS=linux GOARCH=amd64 go build -o dist/$(BINARY_NAME)-linux-amd64 $(LDFLAGS) ./cmd/ihme
	GOOS=linux GOARCH=arm64 go build -o dist/$(BINARY_NAME)-linux-arm64 $(LDFLAGS) ./cmd/ihme
	GOOS=darwin GOARCH=amd64 go build -o dist/$(BINARY_NAME)-darwin-amd64 $(LDFLAGS) ./cmd/ihme
	GOOS=darwin GOARCH=arm64 go build -o dist/$(BINARY_NAME)-darwin-arm64 $(LDFLAGS) ./cmd/ihme
	GOOS=windows GOARCH=amd64 go build -o dist/$(BINARY_NAME)-windows-amd64.exe $(LDFLAGS) ./cmd/ihme

.PHONY: completions
completions: build
	mkdir -p completions
	./$(BINARY_NAME) completion bash > completions/$(BINARY_NAME).bash
	./$(BINARY_NAME) completion zsh > completions/_$(BINARY_NAME)
	./$(BINARY_NAME) completion fish > completions/$(BINARY_NAME).fish

.PHONY: check
check: vet test build
	@echo "All checks passed"

.DEFAULT_GOAL := build
