# Contributing to TraceHub

[English](CONTRIBUTING.md) | [简体中文](CONTRIBUTING.zh-CN.md)

## Sources of truth

- Version scope and acceptance criteria live in `docs/requirements/`.
- GitHub Issues track delivery work and defects.
- Git tags identify released product versions.
- `CHANGELOG.md` is the only changelog.

An Issue cannot add or change version scope. Update and accept the applicable
requirement before implementing new or changed user-visible behavior.

## Branches and pull requests

`main` is the only long-lived branch and must remain releasable. Use a short-lived
`agent/<description>` branch for code, configuration, tests, and release changes.
Those changes must be merged through a pull request. A maintainer may push a
documentation-only correction directly to `main`.

Each pull request must:

- link its Issue and requirement ID when applicable;
- describe user-visible behavior and validation results;
- update tests, documentation, translations, and `CHANGELOG.md` when applicable;
- remove code, configuration, tests, and documentation replaced by the change;
- contain no unrelated changes or secrets.

Pull requests are squash-merged. Delete the branch after merging. The single
maintainer stage does not require another approval; this rule should be changed
to one approval when regular collaborators join the project.

## Issues and requirements

Use Proposal Issues for ideas that are not accepted into a version. Use
Implementation Issues for independently deliverable parts of an accepted
requirement, and assign each one to the target version Milestone. A requirement
may have multiple Implementation Issues. Use Bug Issues for behavior that does
not meet an accepted requirement or documented release behavior.

Requirement IDs use the global `REQ-001` format, are never reused, and have one
of four states: `Draft`, `Accepted`, `Released`, or `Removed`.

## Commits

Use a short Conventional Commit subject:

```text
feat: add Codex session import
fix: reject a truncated session
docs: define v0.1.0 requirements
test: cover duplicate uploads
refactor: simplify session parsing
chore: prepare v0.1.0-alpha.1
```

Keep commits focused. Do not use the commit subject as a substitute for an
Issue, requirement, test result, or changelog entry.

## Definition of done

A change is complete only when its acceptance criteria pass, relevant tests
pass, user-visible behavior is documented, failure boundaries are explicit,
translations are synchronized, and replaced paths have been removed.

See [Release management](docs/releasing.md) for version and release rules.
