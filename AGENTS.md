# TraceHub repository instructions

## Release work

Before changing versions, release requirements, `CHANGELOG.md`, release
workflows, release assets, container publishing, tags, or GitHub Releases:

1. Read `docs/releasing.md` and `docs/releasing.zh-CN.md` completely.
2. Treat `docs/releasing.md` as the normative release policy. If the English
   and Chinese documents differ, stop and synchronize them before continuing.
3. Follow the release checklist in order and stop explicitly when any
   prerequisite is not satisfied.

Release-related code, configuration, and documentation changes must use a
short-lived branch and a pull request. Do not create a release tag until that
pull request is merged and the target `main` commit is confirmed.

Never move, delete, or reuse a published tag. Never build, replace, or upload
release assets manually. Never publish a container outside the documented
release workflow. Never publish a stable release while any included requirement
is not `Released`.
