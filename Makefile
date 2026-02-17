.PHONY: build install test clean

build:
	@mkdir -p bin
	CGO_ENABLED=1 go build -o bin/memo .

install:
	CGO_ENABLED=1 go install .

test:
	CGO_ENABLED=1 go test ./...

clean:
	rm -rf bin
