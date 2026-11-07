# Release management

FloweySales uses three independently versioned deliverables: the application, the Go plugin SDK, and official plugins. Releases are change-driven and gated by test evidence, not by an arbitrary tag alone. This policy implements the first release-pipeline slice tracked by #265 and #266.

## Versioning and cadence

| Component | Source | Tag | Publication |
| --- | --- | --- | --- |
| Application, web UI, backend and MCP | `Doldas/floweysales` | `vMAJOR.MINOR.PATCH` or `vMAJOR.MINOR.PATCH-rc.N` | GitHub Release binary assets and matching GHCR images |
| Go plugin SDK | `Doldas/floweysales/pluginsdk` | `pluginsdk/vMAJOR.MINOR.PATCH` or `pluginsdk/vMAJOR.MINOR.PATCH-rc.N` | SDK Go module tag and GitHub Release record |
| Each plugin | `Doldas/floweysales-plugins` | `<id>-vMAJOR.MINOR.PATCH` | GitHub Release, SHA256 assets and plugin store index |

Aim for a roughly **monthly application stable release**, with earlier patch releases for urgent fixes. The date is a planning target, never a reason to bypass tests. SDK and plugin versions advance **only when those components change**. Use SemVer: major breaking changes, minor additive compatible features, patch compatible fixes. SDK major v2+ requires the Go module path to carry `/v2` (and updated import paths). A wire protocol or plugin manifest schema change must be explicitly versioned and covered by conformance fixtures; SDK SemVer alone does not establish runtime compatibility.

The normal workflow stays trunk-based: short-lived feature/bugfix PRs into `main`, optional short-lived `release/<slug>` stabilization only when necessary. Do not push directly to main, bypass branch protection or tag an unreviewed commit.

## Application release candidate -> stable

1. Complete PR review, policy/Go/frontend CI, integration/browser tests, provider compatibility checks and documentation updates on `main`. Verify the real GitHub branch protection gates; CI running is not the same as CI being mandatory.
2. If needed, create and test an RC tag on an approved commit, e.g. `v0.6.0-rc.1`. The `Release build and publish` workflow validates tag syntax and ancestry, tests the updater, builds Linux amd64/arm64 and Windows amd64 binaries, publishes both `ghcr.io/doldas/floweysales:<tag>` and `ghcr.io/doldas/floweysales-migrate:<tag>` for Linux amd64/arm64 and uploads durable checksummed GitHub Release assets. RC releases are marked prerelease.
3. Test the candidate with `FLOWEYSALES_TAG=<tag>` in an isolated copy of `compose.deploy.yml`. Verify startup, MySQL upgrade from a backup/previous stable schema, login, plugin compatibility, application health and rollback rehearsal. Do not test migration against your only production database.
4. When the candidate passes, create a reviewed stable tag `v0.6.0` from the reviewed `main` commit. Check the GitHub Release, checksums and both versioned image manifests. Never assume a tag implies the workflow has finished.
5. In Actions, run **Promote tested release to stable containers** manually with the stable tag and confirmation `PROMOTE`. Configure the GitHub Environment `stable-release` with required reviewers and deployment protection rules in repository settings. It should require human approval. Promotion moves the migration `stable` image first and application `stable` image last; both retain independent immutable version tags for rollback.
6. Existing hosts following `FLOWEYSALES_TAG=stable` may then pick up the new images via their opt-in host updater. Pinned hosts (`FLOWEYSALES_TAG=vX.Y.Z`) do not move until the operator changes the tag. Never assume a deployment happened merely because a GitHub Release was published.

The workflow does **not** silently deploy into running servers. Release creation and promotion are separate. Do not enable automated stable promotion without a reviewed safety design.

### Container upgrade and rollback

The host updater (`scripts/auto-update.sh`) already pulls *app + migration images only*, backs up MySQL, applies Liquibase migrations, replaces the app, checks `/healthz` and rolls back the app image on failure. Follow `scripts/install-auto-update.sh` for opt-in UI- and timer-triggered updates. Keep `UPDATE_BACKUP=true`, protect backup files, and test a restore before relying on it.

**Important limitations:** Liquibase migrations are forward-only in the current updater; rolling back an image does **not** automatically reverse the DB schema. Database changes must be backward compatible with the previous stable app, or a separately reviewed coordinated maintenance process is required. Mutable `stable` tags are convenient for opt-in auto-updates but are not atomic across app and migration images. For reproducible deployments, pin `FLOWEYSALES_TAG` to a reviewed version and keep both images on the same tag. Do not run `docker compose down -v` during upgrades.

The updater stores user data, secrets and installed provider plugins in persistent volumes. Never publish credentials in release assets, job logs or build arguments.

## SDK release sequence

1. Open a focused PR in `Doldas/floweysales` for SDK or protocol changes. Update `pluginsdk/testdata/contract/` and docs for contract changes. Run the SDK tests and Go backend contract tests.
2. Verify the official plugin consumers still build against the exact prospective SDK commit. The `Plugin SDK release` workflow checks this with a temporary Go module replacement (without modifying the plugin repository).
3. Tag an approved `main` commit `pluginsdk/v0.2.0` (or RC). The SDK workflow independently validates the Go module path, SemVer, vet/race tests, backend tests and official plugin consumer tests before creating its release record.
4. Update `Doldas/floweysales-plugins/go.mod` via a separate reviewed PR to the **stable SDK tag**, run plugin tests, then release only plugins with changes or compatibility fixes. Do not mandate a new version for every plugin when SDK changes remain compatible.
5. Check app runtime/protocol compatibility and store index publication, and record supported SDK, wire protocol and plugin versions in the release notes.

### SDK compatibility matrix, gates and policy

`pluginsdk/compat.json` records which SDK versions speak which wire protocol (currently protocol 1: SDK `>= 0.1.0, < 2.0.0`) and the minimum SDK for official plugins. `node scripts/check-sdk-compat.mjs` fails if the matrix disagrees with `ProtocolVersion` in `pluginsdk/protocol.go`; `--sdk vX.Y.Z [--protocol N]` answers whether a combination is supported and exits non-zero for known-incompatible ones (tested in `scripts/check-sdk-compat.test.mjs`). The app's runtime gate (see `docs/plugins.md`) enforces protocol and version ranges on installed plugins.

Enforced in CI on every PR (`policy` job, `scripts/check-sdk-contract.sh`): a change to `pluginsdk/**/*.go` must also change a contract fixture or test and add a `pluginsdk/CHANGELOG.md` entry, or declare `SDK-Contract: none (reason)` in the PR description. The PR template carries the matching checklist item.

The `Plugin SDK release` workflow additionally:

- compares the exported API with the previous stable SDK tag using `apidiff`. An incompatible change needs a major bump, or in v0 an explicit `Breaking` line in the changelog;
- requires a `## <version>` section in `pluginsdk/CHANGELOG.md` (rename Unreleased in the release PR) and uses it, plus the supported protocol/SDK ranges, as the release notes;
- publishes only after approval of the `sdk-release` GitHub Environment (configure required reviewers in repository settings);
- supports a rehearsal: run it manually (Actions, **Plugin SDK release**, `version_tag` such as `pluginsdk/v0.2.0-rc.1`) to execute every gate on a chosen `main` commit without creating a tag or release.

Go module releases are source tags, so the artifact is reproducible from the tag: `go mod verify` and the tidy check run before publishing, and the module has no third-party dependencies.

**Deprecation and rollback.** Mark a protocol `deprecated` in `compat.json` for at least one minor SDK release and one app release before removing it, and announce it in the changelog. A bad SDK tag is never deleted or moved (the Go module proxy caches it): add a `retract` directive to `pluginsdk/go.mod` in a patch release, keep official plugins pinned to the last good tag, and revert the consumer PR in `floweysales-plugins`.

A `pluginsdk/v*` tag must never trigger the application `v*` workflow. SDK changes do not automatically restart containers.

## Release readiness checklist

- [ ] All included issues have acceptance evidence and reviewed PRs, and required CI checks pass.
- [ ] Release tag has valid SemVer and points to a reviewed commit reachable from `main`.
- [ ] Backend, frontend, SDK, plugins and API/schema compatibility are tested as applicable.
- [ ] User guide, changelog, migration notes and known limitations are current.
- [ ] Application and migration images use the **same** version tag; multi-architecture manifests and downloadable checksums are verified.
- [ ] Upgrade from the previous stable release, backup integrity, health checks and rollback path have been rehearsed.
- [ ] No live trading functionality or broker permission is enabled by a release.
- [ ] Release assets, GitHub Release notes, GHCR access and plugin store status are confirmed.
- [ ] A human approves the promotion to stable after verifying the release.
- [ ] Follow-up incidents can be traced back to commit, release, source data and migration revision.

## Open follow-up work

This is the **first implementation slice**, not completion of all #265/#266 criteria. Future work should add automated migration-upgrade matrix testing, stronger supply-chain attestations and signing, a rehearsed SDK tag and stable consumer upgrade, a container release rehearsal with real disposable MySQL, release-aware update UX and disaster recovery drills. Until those tests are implemented, tag and promotion remain human-reviewed.

## Cached and verified MySQL JDBC dependency (#276)

Liquibase 5.0 does not bundle Connector/J. To avoid Maven Central HTTP 429
when BuildKit evaluates a remote `ADD`, migration images now `COPY` the
locally prepared, **SHA256-pinned** Connector/J 9.5.0 JAR.

For a **local migration image build**, run this once before Docker Compose or
direct Docker builds:

```sh
bash scripts/fetch-liquibase-jdbc.sh
docker compose -f compose.identity.yml --profile migration build migrate
# Or: docker build -f docker/liquibase.Dockerfile -t floweysales-migrate:local .
```

The preparation script verifies existing bytes against the committed
`docker/mysql-connector-j-9.5.0.sha256` and **does not contact Maven Central
on a valid cache hit**. On a miss it downloads the exact pinned version, checks
the downloaded SHA256 before atomically installing it, and fails closed if
anything differs. The image Dockerfile verifies the checksum again during
build. Do not commit the JAR to Git: `docker/vendor/` is ignored.

CI and release containers use the same script and an exact-key GitHub Actions
cache. Cache keys include the pinned checksum file, so intentional driver
upgrades require explicitly updating the version, checksum and references.
Do not substitute upstream-provided, mutable checksums for the reviewed pin.
The cache contains only public verified dependencies, never DB data, tokens or
secrets. The schema `validate`, `update` and `status` tests still run against
a new database even on cache hits. A cold runner can still need Maven access;
release operators should confirm the dependency was cached before tagging.

### Cache scope and measured effect

- **Poisoning:** GitHub Actions caches are scoped by branch. A pull request writes only to its own scope and cannot overwrite the default-branch cache that tag and release builds restore from. Because the script re-verifies the SHA256 of every restored jar, even a tampered cache entry is discarded and refetched, so the cache never decides what is trusted. Docker layer caches (`type=gha`) are intentionally not used: the migration image is tiny, and only the jar was a remote dependency.
- **Measured (2026-11-05, CI runs on this repository):** every run after the cache was introduced restored the jar and logged `Verified cached MySQL Connector/J 9.5.0; no external request needed.` The verify-or-fetch step takes 0 s and cache restore 0-2 s. `identity-schema` takes about 60 s and `container-image` about 91 s, unchanged between runs, because the jar was never the slow part; the gain is that the Maven Central 429 failure mode is gone. Of the CI runs on `main` after #278, all succeeded.
- **Retries:** the migration image build keeps one retry for transient base-image pulls (reduced from four, with a message that no longer blames Maven Central).
