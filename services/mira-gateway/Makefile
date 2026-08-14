APP := mira
IMAGE ?= mira:latest
GOFLAGS ?=

.PHONY: build test docker run lint tidy

build:
	go build $(GOFLAGS) -o bin/$(APP) ./cmd/mira

test:
	go test ./...

docker:
	docker build -t $(IMAGE) .

run:
	go run ./cmd/mira

lint:
	go vet ./...

tidy:
	go mod tidy
