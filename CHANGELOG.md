# Changelog

All notable changes to `pocketnet-node-doctor` are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

_First public release (`v0.1.0`). The capabilities below are the culmination of
roughly three months of development (April–July 2026); the milestone history that
produced them follows under **Development history**._

### Added

- **`diagnose`** — read-only comparison of a local `pocketdb` against a canonical
  manifest, emitting a self-authenticating `plan.json` (format version 2) that
  describes exactly which SQLite pages and whole files diverge.
- **`apply`** — consumes `plan.json`, fetches only the divergent chunks, stages
  and hash-verifies each, atomically swaps them into place, and rolls back to the
  pre-apply state on any verification failure. Resumable across interrupted runs.
- **Delta recovery** — recover a node by downloading only byte-level differences
  from a trusted snapshot instead of the full dataset (a measured ~85.83% page
  reuse, ~4–5× fewer bytes moved, on a representative interval).
- **Production-scale streaming** — both plan emission and consumption stream, so
  peak memory is independent of how much of the node is damaged.
- **Verify-before-side-effect** — `apply` verifies the plan's self-hash before
  creating any staging state or touching a live file; a version gate precedes the
  tamper check, and every hash field is validated before use.
- **Operator-supplied trust root** — `--pinned-hash` anchors verification to a
  canonical manifest you trust; `--canonical` points at a snapshot provider of
  your choice.
- Single static binary for Linux, macOS, and Windows; no runtime dependencies.

---

## Development history

_The tool was built and hardened incrementally before its first public release.
These are the operator-visible milestones, newest first._

### 2026-07 — Production-scale streaming

- Made peak memory **independent of how much of the node is damaged**. Previously,
  at real-world damage levels (millions of changed database pages) the tool
  accumulated the whole recovery plan in memory and was killed by the OS on an
  8 GB node before its first fetch. Both `diagnose` (plan emission) and `apply`
  (plan consumption) now stream the plan: apply's peak memory dropped from
  **~2.7 GB to ~60 MB at 2,000,000 changed pages** (~45× less), and diagnose is
  essentially flat — while the emitted `plan.json` stays byte-for-byte identical.
- **Correctness:** a stale plan of an unrecognized format version is now reported
  as *version-unrecognized* rather than mislabeled *tampered*; a plan carrying a
  malformed hash value is refused with a typed, entry-naming error instead of
  crashing mid-run.

### 2026-06 — Trust and network hardening

- **Verify-before-interpret** on the canonical manifest: the download is hashed
  and checked against the pinned trust root **before** a single byte is parsed,
  with a pre-download capacity check and a size cap.
- **Path-traversal defenses** — manifest entry paths are validated at decode, so
  a malicious manifest cannot direct reads or writes outside the data directory.
- **Transfer resilience for slow/intermittent links** — replaced a single
  wall-clock exchange deadline with phase-scoped timeouts and a stall guard, so a
  recovery on a flaky connection is not aborted by an unrelated slow phase.

### 2026-05 — Recovery pathway, memory hardening, and an end-to-end drill

- **`apply`** landed: fetch → stage → hash-verify → atomically swap → roll back on
  failure, with a shadow copy of every touched file. **Resumable** — an
  interrupted run continues without re-fetching completed work.
- **Content-addressed chunk store** — divergent content is fetched by hash from a
  sharded URL layout, so identical chunks are shared and independently verifiable.
- **Memory hardening** — the manifest and per-page comparison were rewritten to
  stream (bounded memory) instead of loading millions of page records at once,
  so `diagnose` runs on constrained operator hardware.
- **Validated end-to-end** — a damaged node was recovered against a canonical
  snapshot and confirmed healthy, including under simulated intermittent network
  conditions.
- **Operator ergonomics** — `--pinned-hash` trust-root override; a volume-capacity
  preflight that refuses early rather than failing mid-recovery; plans that carry
  their own target path so `apply` works even when `plan.json` lives elsewhere;
  and recovery no longer refuses when the local node is *ahead* of the canonical
  (running the tool is taken as the operator's consent to recover).

### 2026-04 — Delta-recovery model, diagnosis, and the empirical baseline

- Established the core model: **recover a node by downloading only the byte-level
  differences** from a canonical snapshot rather than re-syncing the whole chain.
- Defined the **canonical manifest contract** — tamper-evident, with per-page and
  per-file hashes, a freshness signal, and deterministic serving.
- **`diagnose`** (read-only): compares the local node to the manifest and emits a
  self-authenticating `plan.json`, without modifying any data.
- **Empirical baseline** — measured **~85.83% of database pages reused** across a
  representative real-world interval (~4–5× less data to move), establishing that
  delta recovery is worth building.

[Unreleased]: https://github.com/Pewejekubam/PocketnetNodeDoctor/commits/main
