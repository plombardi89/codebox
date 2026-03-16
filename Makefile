GOCMD=go
GOFMT=gofumpt
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOLINT=golangci-lint run -c .golangci.yaml

.PHONY: codebox clean

codebox:
	$(GOBUILD) -o bin/codebox ./cmd/codebox

lint:
	$(GOFMT) -w .
	$(GOLINT) ./...

clean:
	rm -rf bin/
