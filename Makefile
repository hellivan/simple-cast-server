# simple-cast-server Makefile

REGISTRY ?= ghcr.io/hellivan
TAG      ?= latest
IMG      ?= $(REGISTRY)/simple-cast-server:$(TAG)

.PHONY: all
all: build

.PHONY: build
build:
	go build ./...

.PHONY: test
test:
	go test ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: fmt
fmt:
	gofmt -l cmd internal

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: docker
docker:
	docker build -f build/simple-cast-server.Dockerfile -t $(IMG) .

.PHONY: docker-push
docker-push: docker
	docker push $(IMG)

.PHONY: run
run:
	go run ./cmd/simple-cast-server --config=devices.example.yaml
