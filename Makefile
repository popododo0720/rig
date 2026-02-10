.PHONY: build test vet lint clean

BINARY=rig
VERSION?=dev
LDFLAGS=-ldflags "-X main.version=$(VERSION)"

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/rig

test:
	go test -race ./...

vet:
	go vet ./...

lint: vet
	@echo "Lint passed"

clean:
	rm -f $(BINARY) $(BINARY).exe
