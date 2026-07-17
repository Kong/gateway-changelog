# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Does

A toolchain for validating and generating `CHANGELOG.md` files from individual YAML changelog entries in Kong Gateway repositories. It has two roles:

1. **GitHub Action** (`action.yml`) — validates changelog YAML files against `changelog-schema.json` in CI. On failure, `scripts/explain-changelog-errors.py` prints human-readable GitHub error annotations.
2. **CLI tool** (`changelog generate`) — reads YAML changelog files from a repo, resolves associated PRs/Jiras via the GitHub API, and renders a markdown changelog to stdout using `changelog-markdown.tmpl`.

## Build Commands

```bash
make build      # clean + go generate + go build
make test       # clean + go generate + go test -v ./...
make generate   # go generate (copies template into cmd/ for embedding)
make install    # clean + go generate + go install
```

The `go generate` step (`cmd/generate.go`) copies `changelog-markdown.tmpl` into `cmd/` so it can be embedded via `//go:embed`. The generated copy (`cmd/changelog-markdown.tmpl`) is gitignored — always run `make generate` before building.

## Architecture

- **`main.go`** — entry point, delegates to `cmd.New()` (urfave/cli app).
- **`cmd/root.go`** — CLI app definition with a single `generate` subcommand. Version is set here.
- **`cmd/generate.go`** — core logic: parses YAML entries, resolves each file's original commit via git (handling renames), fetches PR metadata from the GitHub API, extracts Jira IDs from PR bodies, sorts entries by scope priority, and renders the template. Release-line attribution (`releaseLineCandidates` + `fetchCommitContext`): a fix-release branch `next/A.B.C.D` is cut from the minor branch `next/A.B.x.x` and synced by cherry-picking. When `--source-branch` (the minor branch, e.g. `origin/next/A.B.x.x`) is given, every entry is attributed to the PR of its commit **on the minor branch** — the release-line PR, not the upstream/master PR nor the sync PR:
  - introducing commit **is** on the minor branch (present before the cut) → use it as-is;
  - introducing commit is **not** on the minor branch (synced in after the cut) → locate its minor-branch counterpart, in order: (1) `git cherry-pick -x` source, when that source is itself on the minor branch; (2) the minor-branch commit that cherry-picked the same upstream source (`FindCherryPickOnBranch`); (3) the minor-branch commit that introduced the entry's changelog `.yml` file (`FindFileOriginOnBranch`) — the file is cherry-picked verbatim, so it identifies the counterpart even when no `-x` trailer was recorded (e.g. a backport applied without `-x`) and regardless of what else the sync commit touched.

  The introducing commit is always kept as the final fallback (`resolveMergedPR`) so no entry is dropped. Without `--source-branch`, the introducing commit is used directly (backward-compatible).
- **`utils/git.go`** — `FindOriginalCommit` traces a changelog file back through renames to find its original commit SHA. Uses `git log --diff-filter=A` and `git diff-tree -r -M`. `FindCherryPickSource` reads a commit's message and returns the source SHA recorded by `git cherry-pick -x` (`(cherry picked from commit <sha>)`; the last trailer when several hops accumulate). `FindCherryPickOnBranch` finds the commit on a branch that recorded a cherry-pick of a given source SHA (maps a synced-in commit to its minor-branch backport). `FindFileOriginOnBranch` finds the commit on a branch that added a given changelog file — matched at its exact path, then by basename anywhere under `changelog/` — a trailer-independent, diff-independent fallback. `IsAncestor`/`RefExists` gate this on branch reachability (see `cmd/generate.go`).
- **`utils/utils.go`** — `MatchJiras` extracts Jira ticket IDs (prefixes: FTI, AG, KAG, KM, K8, OLLY, KOKO) from PR body text.
- **`changelog-schema.json`** — JSON Schema for changelog YAML. Includes a conditional rule: when `scope` is `"Plugin"`, the `message` must start with one or more bold plugin names (e.g., `**rate-limiting** ...`).
- **`scripts/explain-changelog-errors.py`** — best-effort Python script that re-validates changelogs and emits `::error` annotations with detailed hints (casing mistakes, missing fields, Plugin prefix format).

## Changelog YAML Format

```yaml
message: "Description of the change"        # required, 1-1000 chars
type: feature                                # required: feature|bugfix|dependency|deprecation|breaking_change|performance
scope: Core                                  # optional: Core|Plugin|PDK|Admin API|Performance|Configuration|Clustering|Portal|CLI Command
prs: [1234]                                  # optional: associated PR numbers
githubs: [1234]                              # optional: GitHub issue/PR references
jiras: ["FTI-5678"]                          # optional: Jira ticket IDs (pattern: ^[A-Z]+-[0-9]+$)
```

When `scope: Plugin`, the message **must** start with bold plugin name(s): `**plugin-name** ...` or `**a**, **b**: ...`.

## Key Details

- Requires `GITHUB_TOKEN` environment variable for the `generate` command (needs read access to Contents, Issues, Metadata, Pull Requests).
- Scope priority ordering (lower = rendered first): Performance(10) > Configuration(20) > Core(30) > PDK(40) > Plugin(50) > Admin API(60) > Clustering(70) > Default(100).
- Go version: 1.20. CI runs on `ubuntu-latest`.
- Releases build cross-platform binaries (linux/darwin/windows × amd64/arm64) via GitHub release trigger.
