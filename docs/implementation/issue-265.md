# Issue #265: release asset completeness

Status: implemented on this PR branch; no release was published or deployed.

The GitHub Release job now rejects any missing or empty backend/MCP binary for Linux amd64, Linux arm64 or Windows amd64 before generating checksums and uploading assets. This is a release-time guard, not proof that release upgrade and rollback have been rehearsed.

Validation: GitHub Actions YAML parse and release workflow dry run on reviewed tags. **Do not create an app release tag from this branch solely to test this PR.** Parent issue remains open for upgrade and RC validation.
