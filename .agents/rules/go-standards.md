# Go Coding Standards

Go coding standards for this project:

- Follow standard Go project layout: `cmd/` for entrypoints, `internal/` for private packages
- Use Go naming conventions (camelCase for unexported, PascalCase for exported)
- Error handling: always handle errors, never use `_` for error values, wrap errors with `fmt.Errorf("context: %w", err)`
- Logging: use structured logging (`slog` or `zerolog`), never `fmt.Println` in production code
- Context: pass `context.Context` as first parameter to all functions that do I/O
- Configuration: all configurable values loaded from `configs/config.yaml`, never hardcoded
- Testing: table-driven tests, test files in same package with `_test.go` suffix
- Dependencies: prefer stdlib over third-party, justify new dependencies
- Comments: exported functions must have godoc comments
- Linting: code must pass `go vet`, `go build`, and ideally `golangci-lint`
