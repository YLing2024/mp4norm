.PHONY: help build test vet fmt bench gui fetch-ffmpeg

help:
	@echo "targets: build test vet fmt bench gui fetch-ffmpeg"

build:
	go build -o bin/mp4norm ./cmd/mp4norm

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

bench:
	go test ./internal/normalize/ -run '^$$' -bench . -benchmem

gui:
	cd gui && wails build

fetch-ffmpeg:
ifeq ($(OS),Windows_NT)
	powershell -ExecutionPolicy Bypass -File scripts/fetch-ffmpeg.ps1
else
	bash scripts/fetch-ffmpeg.sh
endif
