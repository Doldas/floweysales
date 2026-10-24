# FloweySales Userspace — architecture and rollout

## Purpose
Userspace is a **tenant-owned virtual document store** accessed by the app, saved flows and (later) scoped AI tools. Userspace paths (e.g. `/research/2026-10/report.md`) are logical identifiers, **not host filesystem paths**. Folders are inferred from prefixes in v1; empty folders are represented by a `<folder>/.folder` marker.

## Implemented and merged
- Authenticated userspace CRUD API under `/api/userspace`; every database query is scoped to the authenticated principal. Admin flows resolve the **saved flow's creator** as the userspace principal, not the caller's session or an AI-supplied owner ID.
- MySQL-backed metadata and content, provisioned by Liquibase migration 012.
- Simple text formats: Markdown, plain text, CSV and JSON, editable from My Workspace in Flow Studio.
- `workspace_read` and `workspace_write` flow nodes. Read loads text from the flow creator's userspace; write stores the preceding result (text/plain) with an explicit path.
- Logical path validation forbids `.`, `..`, NUL, backslash, relative paths. No host path access, code execution or general SQL exposed.
- Per-file 2 MiB limit, per-user 256 file limit, concurrent-write quota serialization using the owning auth row. Deletes are owner scoped.
- File downloads sent as `application/octet-stream` and `nosniff` to avoid displaying untrusted HTML as active content.
- **Folder deletion (merged PR #103):** My Workspace can delete a folder and all nested files after showing a confirmation and affected-file count. The authenticated endpoint deletes owner-scoped paths in one transaction while locking the owner account row; deleting `/` is rejected, and `/reports/` never matches `/reports-old/`. Deletion is permanent, without trash/restore.

## Deliberate limits of the first slice
This is **not** a finished enterprise document-management product: no persistent SQLite database workspaces, no explicit ACL sharing, no version history, retention policies, backups or virus scanning, and no MCP write tools yet. Files are small text payloads stored in MySQL; this is not arbitrary binary upload or a general-purpose mounted filesystem. More granular per-flow permissions and an approval model for writes are required before granting agentic models write access.

## Next design increments (separate reviewable PRs)
1. Object storage adapter (S3/MinIO) with streaming uploads, transactional metadata, checksum verification, MIME restrictions, encryption/KMS, retention and audit events.
2. Explicit folder objects, rename/move, trash/restore, pagination, search, project-scoped ACL and quotas per organization. Simple inferred-folder recursive deletion already exists.
3. Workspace SQLite service, **not** raw file writes in the Go API. Provision SQLite databases behind a broker that serializes mutations, uses allowlisted SQL/parameter binding, isolates owners, enforces CPU/time/storage limits, and provides snapshots/backups. Never open arbitrary userspace paths as SQLite files.
4. Capability-scoped flow tools (`file.read`, `file.write`, `database.query`, `database.mutate`) and a **separate** MCP flow-tooling server that uses the same policy enforcement and audit trail. Read-only tools by default; writes require explicit tool grants and approval semantics. No arbitrary shell, file-path or SQL execution for agents.
5. Idempotency keys, versioned file writes, lifecycle/GC, trace correlation and comprehensive tests for concurrency, auth boundaries and workflow recovery.

## Endpoints (v1)
- `GET /api/userspace/files`
- `GET /api/userspace/file?path=/research/report.md`
- `PUT /api/userspace/file` with JSON `{"path":"/research/report.md","content_type":"text/markdown","content":"# Report"}`
- `DELETE /api/userspace/file?path=/research/report.md`
- `DELETE /api/userspace/folder?path=/research` — permanently removes files beneath `/research/`, returning `{"deleted":true,"files_deleted":N}`; owner-scoped, root rejected, 404 if no files matched.

All endpoints require a valid session. Existing Flow Studio remains SYS_ADMIN-restricted.

For the full October 2026 progress inventory, see [Flow Studio delivery status](flow-studio-progress.md).
