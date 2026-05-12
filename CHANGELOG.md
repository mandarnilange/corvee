# Changelog
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.0] - 2026-05-12
First public release. Single Go binary (≥1.26), no daemon, no server; state lives in `.tasks/` and syncs across machines via plain `git push` / `git pull`.

### Added
- Core local-first task tracking commands: `init`, `add`, `list`, `show`, `update`, `done`, and `delete`
- Hierarchy, validation, graph/index support, summary/tree views, and clone/reindex flows
- Distributed-safe leasing, claim/release/heartbeat/reap flows, and idempotent operation handling
- Multi-file operations with recovery, schema migration, and crash-injection test coverage
- Real-time observability via `watch`, structured journal metadata, and relative `--since` parsing
- Static HTML dashboard rendering (board / tree / summary views) and deploy manifest generation
- Git sync, deterministic merge handling, import/export, and explicit `sync --watch`
- Plugin protocol, shell hooks, compaction, and skill/reference documentation
- AI-agent skills: `corvee` (day-to-day claim/heartbeat/done loop) and `corvee-install` (onboarding, version check, upgrade, agent-doc bootstrap)
- Cross-compiled release artifacts for `linux/amd64`, `linux/arm64`, `darwin/arm64` plus a publishable skill tarball, all attached to the GitHub Release
- OSS community scaffolding: Code of Conduct, Security Policy, issue templates, and PR template

### Changed
- The binary and project naming were standardized on `corvee`
- The README and contributor-facing docs were expanded for open-source publication readiness

[Unreleased]: https://github.com/mandarnilange/corvee/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/mandarnilange/corvee/releases/tag/v1.0.0
