# pocketnet-node-doctor

**Recover a dead or corrupted [Pocketnet](https://pocketnet.app) node by downloading only the byte-level *differences* from a canonical snapshot — not the whole chain.**

A full resync pulls the entire ~60 GB dataset. `pocketnet-node-doctor` compares your local node against a trusted canonical snapshot and fetches only the pages and files that actually differ — typically well under a quarter of the data (in one measured March→April interval, **85.83% of database pages were reused**, roughly a **4–5× reduction** in bytes moved). On a slow or metered link, that is the difference between a recovery that finishes overnight and one that doesn't.

It runs as a **single static binary** with no runtime dependencies on Linux, macOS, and Windows, and is built to work on modest operator hardware (8 GB RAM, HDD, ~10 Mbps) at real production scale — millions of changed database pages — without exhausting memory.

---

## How it works

Recovery is two operator-invoked steps:

1. **`diagnose`** — read-only. Compares your local `pocketdb` against the canonical manifest and writes a machine-readable **`plan.json`** describing exactly which pages and files diverge. It never touches your data.
2. **`apply`** — consumes `plan.json`, fetches only the divergent chunks, **stages** them, verifies each against its expected hash, then **atomically swaps** them into place. Every change is shadow-copied first; if post-apply verification fails, it **rolls back** to the pre-apply state. The plan is self-authenticating (a `self_hash` over its own bytes), and apply verifies that hash **before** making any change on disk.

Both ends stream the plan, so peak memory is independent of how much of your node is damaged.

---

## Requirements

- **Go 1.25.0 or newer** to build from source (the toolchain version is pinned).
- A **canonical snapshot endpoint** you trust — a manifest URL plus its expected SHA-256. `pocketnet-node-doctor` does not ship a default endpoint; you point it at a snapshot provider you trust (your own, or one your community operates). See [Trust root](#trust-root).
- A stopped node. `diagnose` refuses to run against a live node (exit code 2); stop `pocketcoind` first, and — always — snapshot or back up the datadir before recovery.

---

## Install

### Build from source (recommended)

```sh
git clone https://github.com/Pewejekubam/PocketnetNodeDoctor.git
cd PocketnetNodeDoctor
go build -o pocketnet-node-doctor ./cmd/pocketnet-node-doctor
./pocketnet-node-doctor --version
```

### Release binaries

Pre-built static binaries for common OS/arch targets are attached to each [GitHub Release](https://github.com/Pewejekubam/PocketnetNodeDoctor/releases). Download the one for your platform, `chmod +x`, and run — no toolchain required.

> **Note:** `go install` is not supported (the module path and the repository owner differ). Build from source or use a Release binary.

---

## Usage

### 1. Diagnose (read-only)

```sh
pocketnet-node-doctor diagnose \
  --canonical  https://your-snapshot-provider.example/manifest.json \
  --pinned-hash <expected-sha256-of-the-manifest> \
  --pocketdb   /path/to/pocketnet/datadir \
  --plan-out   ./plan.json
```

| Flag | |
|---|---|
| `--canonical <url>` | **required** — URL of the canonical manifest |
| `--pocketdb <path>` | **required** — your local Pocketnet data directory |
| `--plan-out <path>` | where to write `plan.json` (default: `<pocketdb-parent>/plan.json`) |
| `--pinned-hash <hex>` | expected SHA-256 of the manifest — overrides the compiled-in trust root (see below) |
| `--verbose` | debug output on stderr |

`diagnose` is read-only and safe to re-run. It exits non-zero and explains itself if it refuses (e.g. a node is running, the volume lacks capacity, or `pocketdb` is read-only).

### 2. Apply (mutating — back up first)

```sh
pocketnet-node-doctor apply --plan ./plan.json
```

| Flag | |
|---|---|
| `--plan <path>` | **required** — the `plan.json` produced by `diagnose` |
| `--parallel <n>` | parallel fetch workers, `1..32` (default `4`) |
| `--verbose` | debug output on stderr |

`apply` verifies the plan's self-hash before any change, stages and hash-checks every fetched chunk, swaps atomically, and rolls back on verification failure. It is **resumable** — re-running the same plan skips already-completed work.

---

## Trust root

The plan and every fetched chunk are verified against hashes anchored to the **canonical manifest**. You establish that anchor with `--pinned-hash <hex>` — the SHA-256 you expect the manifest to have from a source you trust (published height announcement, a provider you run, a value your community agrees on).

Binaries built plainly from source carry a **development** trust-root default, which is **not** a production pin. For real recovery, always pass `--pinned-hash` explicitly (or use a Release binary, which is built with a production pin injected at release time). `--version` prints the trust root the binary is currently using.

---

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | generic error / invalid input |
| 2 | refused — a Pocketnet node is running |
| 4 | refused — pocketnet-core version mismatch |
| 5 | refused — insufficient volume capacity |
| 6 | refused — `pocketdb` is read-only |
| 7 | refused — manifest/plan format version unrecognized |
| 10 | apply rolled back to the pre-apply state |
| 11 | rollback itself failed (manual restore needed) |
| 12 | network budget exhausted — re-run to resume |
| 14 | plan superseded — the canonical moved on; re-diagnose |
| 15 | plan tampered — self-hash mismatch, nothing applied |

---

## Safety model

- **Read-only diagnose.** Planning never modifies your node.
- **Verify before side effect.** Apply confirms the plan's self-hash before creating any staging state or touching a live file.
- **Stage, verify, swap, roll back.** Each chunk is fetched to staging, hash-checked, then atomically swapped; a shadow copy of every touched file enables full rollback on failure.
- **Resumable.** An interrupted apply resumes from where it stopped without re-fetching completed work.
- **You own the trust anchor.** The tool never trusts a snapshot you didn't pin.

Always snapshot or back up the datadir before running `apply`.

---

## Building & testing

```sh
go build ./...
go vet ./...
go test ./...
```

`scripts/check.sh` runs the same gate used in CI (`gofmt` / `vet` / `test`).

---

## License

[MIT](LICENSE) © 2026 Pewe Jekubam.

Contributions welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). Security reports: see [SECURITY.md](SECURITY.md).
