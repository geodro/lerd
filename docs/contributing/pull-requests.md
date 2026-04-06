# Submitting a Pull Request

## Before opening a PR

### Formatting

All Go code must be formatted with `gofmt`. Check and fix before committing:

```bash
gofmt -l .   # list files with issues
gofmt -w .   # fix them
```

The CI will reject PRs with unformatted code.

### Tests

Run the full test suite:

```bash
make test
```

**New functionality must include tests.** PRs that add or change behaviour without corresponding test coverage will not be merged.

### Vet

```bash
CGO_ENABLED=1 go vet ./...
```

## CI checks

Every PR runs the following checks automatically. All must pass before merging.

| Check | Command |
|-------|---------|
| Build | `go build ./cmd/lerd` |
| Tests | `go test ./...` |
| Vet | `go vet ./...` |
| Format | `gofmt -l .` |
| Installer tests | `bats tests/installer/installer.bats` |
