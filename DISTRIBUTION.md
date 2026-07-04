# Distribution

This repository is the **public distribution package** for
`pocketnet-node-doctor`: the auditable Go source and released binaries that
Pocketnet node operators build, run, and review.

## What this repo is

- The complete, buildable source for the `diagnose` and `apply` tools
  (`cmd/`, `internal/`, `tests/`).
- Released static binaries, attached as assets to each
  [GitHub Release](https://github.com/Pewejekubam/PocketnetNodeDoctor/releases).

## What this repo is not

It is the **artifact, not the blueprint**. The private build environment used to
develop and validate the tool — its internal tooling, infrastructure, and
process automation — is not part of this package and is not published here. That
separation keeps the public repository clean and auditable: everything present
builds and runs on its own, and nothing here depends on anything that isn't here.

## Auditability

The tool atomically swaps a node's on-disk data. It earns trust by being
readable end-to-end: every fetch is hash-verified against a manifest you pin,
every change is staged and rolled back on failure, and the full source is here to
review. If you can build it, you can audit it.
