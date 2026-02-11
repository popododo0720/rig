.PHONY: build test vet lint clean install serve docker-build docker-push help

BINARY=rig
VERSION?=dev
LDFLAGS=-ldflags "-X main.version=$(VERSION)"
DOCKER_IMAGE?=rigdev/rig
DOCKER_TAG?=$(VERSION)

# Default target
.DEFAULT_GOAL := help

## build: Build the rig binary
build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/rig

## test: Run all tests (without -race on Windows due to gcc requirement)
test:
	go test ./...

## vet: Run go vet on all packages
vet:
	go vet ./...

## lint: Run linting checks (go vet)
lint: vet
	@echo "Lint passed"

## clean: Remove build artifacts
clean:
	rm -f $(BINARY) $(BINARY).exe

## install: Install binary to $GOPATH/bin
install: build
	go install ./cmd/rig

## serve: Build and run (web + webhook)
serve: build
	./$(BINARY) serve -c rig.yaml

## docker-build: Build Docker image
docker-build:
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .
	docker tag $(DOCKER_IMAGE):$(DOCKER_TAG) $(DOCKER_IMAGE):latest

## docker-push: Push Docker image to registry
docker-push: docker-build
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)
	docker push $(DOCKER_IMAGE):latest

## help: Show this help message
help:
	@echo "Available targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
