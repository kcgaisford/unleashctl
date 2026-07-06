.PHONY: codegen build test

UNLEASH_URL ?= http://localhost:4242

OAPI_CODEGEN := $(shell go env GOPATH)/bin/oapi-codegen

codegen:
	curl -s $(UNLEASH_URL)/docs/openapi.json -o api/openapi.json
	python3 api/filter_spec.py
	@if [ ! -x "$(OAPI_CODEGEN)" ]; then go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest; fi
	$(OAPI_CODEGEN) -config api/codegen.yaml api/openapi.min.json

build:
	go build ./...

test:
	go build ./... && go vet ./... && go test ./...
