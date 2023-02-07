default: testacc

# Build the provider
build:
	mkdir -p bin/
	CGO_ENABLED=0 go build \
		-o bin/terraform-provider-googlesiteverification

PROVIDER ?= registry.terraform.io/tinyzimmer/googlesiteverification
VERSION ?= 0.1.0
install:  export OS_ARCH=$(shell go env GOHOSTOS)_$(shell go env GOHOSTARCH)
install: build
	mv bin/terraform-provider-googlesiteverification ~/.terraform.d/plugins/$(PROVIDER)/$(VERSION)/$(OS_ARCH)

# Run acceptance tests
.PHONY: testacc
testacc:
	TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 120m
