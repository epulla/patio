.PHONY: web build release test

web:
	pnpm --dir web install
	pnpm --dir web build

build: web
	go build -o bin/my-tickets .

release:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o bin/my-tickets-windows-amd64.exe .
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o bin/my-tickets-darwin-arm64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o bin/my-tickets-darwin-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/my-tickets-linux-amd64 .

test:
	go test ./...
