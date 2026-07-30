VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BIN := bin/ghx

.PHONY: build run install clean tidy test vet lint

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BIN) .

run: build
	./$(BIN)

install: build
	rm -f ~/.local/bin/ghx
	cp $(BIN) ~/.local/bin/ghx

clean:
	rm -rf bin/

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

tidy:
	go mod tidy
