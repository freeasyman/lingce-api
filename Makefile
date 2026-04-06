.PHONY: build run test lint clean build-linux help

APP_NAME := lingce-api
BUILD_DIR := bin
MAIN_PATH := ./cmd/lingce-api

help:
	@echo "Available targets:"
	@echo "  build        - Build the application"
	@echo "  run          - Build and run the application"
	@echo "  test         - Run tests"
	@echo "  lint         - Run linter"
	@echo "  clean        - Remove build artifacts"
	@echo "  build-linux  - Cross-compile for Linux amd64"

build:
	@echo "Building $(APP_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(APP_NAME) $(MAIN_PATH)

run: build
	@echo "Running $(APP_NAME)..."
	$(BUILD_DIR)/$(APP_NAME)

test:
	@echo "Running tests..."
	go test ./... -v -count=1

lint:
	@echo "Running linter..."
	golangci-lint run ./...

clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)

build-linux:
	@echo "Cross-compiling for Linux amd64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(APP_NAME) $(MAIN_PATH)
