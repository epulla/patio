# my-tickets

Local Go + SQLite ticket app with embedded React shell.

## Development

```sh
go run . -port 8080
pnpm --dir web install && pnpm --dir web dev
```

The Go server stores its database under the OS user config directory.

## Build

```sh
make build
```

Run `bin/my-tickets`. Use `make release` for Windows, macOS, and Linux builds.
