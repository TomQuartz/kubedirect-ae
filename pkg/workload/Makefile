.PHONY: proto build

ROOT_DIR := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))

proto:
	protoc \
		--go_out=. \
		--go_opt=paths=source_relative \
		--go-grpc_out=. \
		--go-grpc_opt=paths=source_relative \
		proto/faas.proto
