# Changelog
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project intends to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html) once public tags begin.

## [Unreleased]
### Added
- Core local-first task tracking commands: `init`, `add`, `list`, `show`, `update`, `done`, and `delete`
- Hierarchy, validation, graph/index support, summary/tree views, and clone/reindex flows
- Distributed-safe leasing, claim/release/heartbeat/reap flows, and idempotent operation handling
- Multi-file operations with recovery, schema migration, and crash-injection test coverage
- Real-time observability via `watch`, structured journal metadata, and relative `--since` parsing
- Static HTML dashboard rendering and deploy manifest generation
- Git sync, deterministic merge handling, import/export, and explicit `sync --watch`
- Plugin protocol, shell hooks, compaction, and skill/reference documentation
- OSS community scaffolding: Code of Conduct, Security Policy, issue templates, and PR template

### Changed
- The binary and project naming were standardized on `corvee`
- The README and contributor-facing docs were expanded for open-source publication readiness

### Notes
- No public release tag has been cut yet.
- Versioned release sections will start once the repository moves to its public home and the first tag is created.
