# Release Management

[English](releasing.md) | [简体中文](releasing.zh-CN.md)

## Version policy

TraceHub uses one [Semantic Version](https://semver.org/spec/v2.0.0.html) for the
entire repository. Components and agent adapters do not have independent product
versions. Published Git tags are the source of truth for released versions.

- Alpha tags use `v0.1.0-alpha.1`, incrementing the final number for each build.
- Stable tags use `v0.1.0`.
- No beta or release-candidate channel is used during the first development stage.
- Before `v1.0.0`, a minor version may contain incompatible changes.
- A patch version fixes existing behavior and must not intentionally break the
  same minor version. New product capability requires a minor version.

Requirement files stay in Draft or Accepted state through Alpha releases. A
stable release requires all included requirements to be Accepted before the
release and changed to Released by the release pull request.

## Changelog

`CHANGELOG.md` is the only changelog and is maintained in English. A pull request
that changes user-visible behavior updates the applicable Unreleased category.
Internal changes that do not affect users do not require an entry.

## Manual release checklist

1. Create a release branch and pull request from current `main`.
2. Confirm the target requirements, linked Issues, tests, documentation,
   translations, and clean-install instructions.
3. Move relevant Unreleased entries to the exact version and release date.
4. For a stable release, mark its requirements Released and close the Milestone.
5. Squash-merge the release pull request into `main`.
6. Create an annotated tag on the resulting `main` commit.
7. Create a GitHub Release. Mark an Alpha as a prerelease and list completed and
   incomplete requirements.
8. Upload immutable artifacts and a SHA-256 checksum file.
9. Install from only the published artifacts and run the documented core flow on
   every platform declared by the release.

Do not create a stable `v0.1.0` release until the installable artifacts complete
the Codex submission, cross-device discovery, and session-reading flow on more
than one device.

## Failed releases and rollback

Never move or delete a published tag to reuse its version, and never replace a
published asset with different bytes under the same name. If a release is bad,
mark it as affected in the Release notes, restore the last known good version in
affected environments, fix the issue on a new branch, and publish the next Alpha
or patch version. Each replacement artifact receives a new version and checksum.
