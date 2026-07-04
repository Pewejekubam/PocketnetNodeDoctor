# Contributing

Thanks for helping improve `pocketnet-node-doctor` — a tool the Pocketnet
operator community relies on to recover nodes.

## Ground rules

- **Open an issue first** for anything non-trivial, so we can agree on the
  approach before you invest time.
- Keep changes focused. One logical change per pull request.
- This tool mutates a node's data directory. Changes to the verification,
  staging, atomic-swap, or rollback paths must come with tests that exercise the
  failure modes, not just the happy path.

## Development

Requires **Go 1.25.0+**.

```sh
go build ./...
go vet ./...
go test ./...
```

`scripts/check.sh` runs the same `gofmt` / `vet` / `test` gate as CI. A pull
request must be green under it (`gofmt` clean, no `vet` findings, all tests
passing) before review.

## Pull requests

- Describe **what** changed and **why**, and how you verified it.
- Add or update tests alongside behavior changes.
- Keep the public CLI surface and exit-code contract stable unless the change is
  explicitly about them (and called out in the PR).

By contributing you agree that your contributions are licensed under the
project's [MIT License](LICENSE).
