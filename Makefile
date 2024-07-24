.PHONY: build deploy clean

GOARCH ?= $(shell go env GOARCH)

GOBUILD=CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH)

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -mod=vendor -o bin/raccoon ./cmd/raccoon/raccoon.go
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -mod=vendor -o bin/raccoond ./cmd/raccoond/raccoond.go

REGISTRY=layzer/raccoon

VERSION=$(shell git rev-parse --short HEAD)

docker-build: build
	docker build -t $(REGISTRY):$(VERSION) .

deploy:
	kubectl apply -f deploy/raccoon.yaml

clean:
	rm -rf bin
	go mod tidy
	go mod vendor

kind-cluster:
	kind create cluster --config deploy/kind.yaml

kind-image-load: docker-build
	kind load docker-image $(IMG):$(VERSION) 