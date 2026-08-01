# Contributing

Thanks for contributing to `go-bacnet`.

## Requirements

- Go **1.23** or newer
- Always run toolchain commands with module isolation when a parent `go.work`
  may be present:

```bash
export GOWORK=off
```

The Makefile exports `GOWORK=off` for you.

## Local checks

```bash
make check
```

`check` runs format, tidy, vet, staticcheck, **golangci-lint** (same gate as
CI, including `errcheck`), vulncheck, unit tests, race tests, coverage, and
the import-boundary test. Install the CI tools once:

```bash
go install honnef.co/go/tools/cmd/staticcheck@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install golang.org/x/vuln/cmd/govulncheck@latest
```

Useful individual targets: `make test`, `make test-race`, `make vet`,
`make lint`, `make lint-ci`, `make fmt`, `make tidy`, `make vuln`,
`make coverage`, `make fuzz`, `make imports`, `make interop`,
`make build-cmd`.

## Releases

[RELEASE.md](RELEASE.md) is **release notes only**: title, then newest
`## vX.Y.Z` section first. The shared release workflow parses from the top of
that file — do not put versioning policy or how-to text above the latest tag.

To cut a release:

1. Add/update the `## vX.Y.Z` section at the top of `RELEASE.md` (after the title)
2. Merge to `main`
3. Run **Actions → Release → Run workflow** with the bump (`patch` / `minor` /
   `major`). First tag: use **`minor`** for `v0.1.0`
4. The workflow tags the module, publishes the GitHub release, and attaches
   `bacnetctl` binaries

Do not push tags or create GitHub releases locally unless that is explicitly
requested.

## Import boundaries

Package dependency direction is documented in
[docs/PACKAGE_DESIGN.md](docs/PACKAGE_DESIGN.md) and enforced by
`internal/imports`. Do not introduce cycles or cross-import sibling codecs.

Root `bacnet` stays transport-neutral: no `bip` / wire / client imports.

## Style

- SPDX header on every first-party `.go` file: `// SPDX-License-Identifier: MIT`
- Prefer sentinel / typed errors with `%w`; see [ERRORS.md](ERRORS.md)
- Keep diffs focused; update tests and docs when behaviour changes
- Do not claim real-device interoperability without evidence in
  [docs/REAL_DEVICE_GATE.md](docs/REAL_DEVICE_GATE.md)

## Pull requests

1. Discuss non-trivial API changes first
2. Add or update tests
3. Run `make check` (or at least `GOWORK=off go test ./...` and `go vet ./...`)
4. Open a PR with a clear summary and test notes
