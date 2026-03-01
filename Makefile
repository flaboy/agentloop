.PHONY: help test fmt vet lint validate-lint

help:
	@echo "Targets:"
	@echo "  make test           - Run go test ./..."
	@echo "  make fmt            - Run gofmt on all Go files"
	@echo "  make vet            - Run go vet ./..."
	@echo "  make lint           - Run golangci-lint"
	@echo "  make validate-lint  - Run fmt check + vet + golangci-lint"

test:
	go test ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

vet:
	go vet ./...

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed."; \
		exit 1; \
	fi

validate-lint:
	@echo ">> verifying gofmt formatting"
	@gofmt -l $$(find . -name '*.go' -not -path './vendor/*') | tee /tmp/agentloop-gofmt.out
	@test ! -s /tmp/agentloop-gofmt.out
	@echo ">> running go vet"
	@go vet ./...
	@echo ">> running golangci-lint"
	@$(MAKE) lint
