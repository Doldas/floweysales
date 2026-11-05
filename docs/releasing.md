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

This is the **first implementation slice**, not completion of all #265/#266 criteria. Future work should add automated migration-upgrade matrix testing, stronger supply-chain attestations and signing, explicit compatibility manifest/validation, a container release rehearsal with real disposable MySQL, release-aware update UX and disaster recovery drills. Until those tests are implemented, tag and promotion remain human-reviewed.

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
