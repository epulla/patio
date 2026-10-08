.PHONY: web build release test

web:
	pnpm --dir web install
	pnpm --dir web build

build:
	wails build

release:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /dev/null .

test:
	go test ./...
