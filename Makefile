GOCMD=go
GOFMT=gofumpt
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOLINT=golangci-lint run -c .golangci.yaml

.PHONY: codebox azure-token-inspect clean

default: fmt lint codebox

codebox:
	$(GOBUILD) -o bin/codebox ./cmd/codebox

azure-token-inspect:
	$(GOBUILD) -o bin/azure-token-inspect ./hack/cmd/azure-token-inspect

fmt:
	$(GOLINT) --fix -E wsl_v5 ./...
	$(GOFMT) -w .

lint: fmt
	$(GOLINT) ./...

clean:
	rm -rf bin/
