# Security Policy

## Reporting a vulnerability

Please report security vulnerabilities **privately** via GitHub Security
Advisories:

> Repository → **Security** tab → **Report a vulnerability**

Do not open a public issue for a security report.

Because `pocketnet-node-doctor` writes to a node's data directory, we take
reports about the verification, staging, swap, and rollback paths especially
seriously. Helpful reports include the version (`pocketnet-node-doctor --version`),
your OS/arch, and the minimal steps to reproduce.

## Supported versions

The latest released version receives security fixes. Until a `1.0.0` release,
only the most recent tag is supported.

## Trust model

Verification is anchored to a canonical manifest **you** pin (`--pinned-hash`).
The tool never trusts a snapshot endpoint or manifest you have not explicitly
pinned. Plans are self-authenticating and are verified before any change is made
to disk.
