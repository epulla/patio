.PHONY: web app exe linux release test

VERSION ?= dev
# Info.plist needs a numeric version; dev builds get 0.0.0.
BUNDLE_VERSION = $(or $(patsubst v%,%,$(filter v%,$(VERSION))),0.0.0)
GO_BUILD = go build -tags production -trimpath
LDFLAGS = -w -s -X main.version=$(VERSION)
APP = build/bin/patio.app

web:
	pnpm --dir web install
	pnpm --dir web build

app: export CGO_ENABLED = 1
# Go 1.27 links for macOS 13+.
app: export MACOSX_DEPLOYMENT_TARGET = 13.0
app: export CGO_CFLAGS = -mmacosx-version-min=13.0
app: export CGO_LDFLAGS = -mmacosx-version-min=13.0
app: web
	rm -rf $(APP)
	mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources
	$(GO_BUILD) -ldflags "$(LDFLAGS)" -o $(APP)/Contents/MacOS/patio .
	sed 's/@VERSION@/$(BUNDLE_VERSION)/g' build/darwin/Info.plist > $(APP)/Contents/Info.plist
	cp build/darwin/icons.icns $(APP)/Contents/Resources/
	codesign --force --deep --sign - $(APP)

exe: export CGO_ENABLED = 0
exe: export GOOS = windows
exe: export GOARCH = amd64
exe: web
	$(GO_BUILD) -ldflags "$(LDFLAGS) -H windowsgui" -o build/bin/patio.exe .

linux: export CGO_ENABLED = 1
linux: web
	$(GO_BUILD) -ldflags "$(LDFLAGS)" -o build/bin/patio .

release:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /dev/null .

test:
	go test ./...
