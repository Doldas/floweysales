---
title: Install, run and back up
order: 15
summary: For administrators: the ways to run FloweySales, what must be backed up, how updates work and what to protect.
last-reviewed: 2026-11-06
---

# Install, run and back up

This page is for the person who runs FloweySales. It gives the overview and points to the technical notes for commands. If someone else runs it for you, you do not need it.

## Ways to run it

| Way | Use it for | Where the steps are |
| --- | --- | --- |
| **From source** with `make run` | Trying it on your own computer. It builds the web app and the Go program and serves both on port 8080. Needs Go and Node.js with npm. | [Getting started](getting-started.md) |
| **Container stack** with Docker or Podman Compose | A repeatable setup with a MySQL database for accounts and a one-shot database migration step. | [Docker and Podman development stack](../containers.md) |
| **Published images** with `compose.deploy.yml` | Running released images, with optional automatic updates. | [Automatic updates](../auto-updates.md) |

Open `http://localhost:8080` when the server is up. The app is also installable as a web app from the browser.

## Accounts and sign-in

Authentication is off by default. With it off, one local user has all the access of an administrator, which is fine on your own computer and wrong on a shared one. To give other people access, turn authentication on with a MySQL database for accounts. The first administrator is created on first start from bootstrap values you set once, and only if no account exists yet. Remove the bootstrap password afterward. There is no public sign-up; an administrator creates every account. Serve the app over HTTPS before exposing it beyond your own computer. Details: [Database and authentication](../database-and-auth.md).

## After the first start

1. Connect an AI endpoint under **Settings**, **API integrations**.
2. Install a market-data provider under **Settings**, **Plugins**. Without one the data screens are empty.
3. Optionally prepare model profiles, mail (for invitations and alerts) and trading limits for the paper portfolio.

[Settings and roles](settings.md) describes each tab. Configuration can also come from environment variables or a `config.properties` file; `config.properties.example` in the repository lists the names.

## What to back up

- **The MySQL database** with accounts, roles and settings.
- **The SQLite file** that holds the paper portfolio and market history. Where it lives is set by `SQLITE_STORAGE_PATH`. The example configuration uses `:memory:`, which keeps nothing after a restart, so set a file path if you want to keep this data.
- **The secrets key.** Integration credentials are encrypted with a key. If you do not provide one, the server creates it in a local file and reuses it. Back it up together with the database, and do not delete or replace it: stored credentials could no longer be decrypted. In the container setups it is on the `secrets_data` volume.
- **The plugin directory**, or reinstall providers from Settings.

## Updates

**Settings**, **Updates** shows the installed build and whether a newer one exists. When the host updater from [Automatic updates](../auto-updates.md) is installed, an administrator can request an update from there; it takes a database backup first and restores the previous version if the new one fails to start. Without the host updater you update by pulling the new images or rebuilding from source. The database server itself is never updated automatically.

## Protecting the system

- Treat the AI key, the mail password and the secrets key as secrets; they are never shown again after saving.
- Market-data plugins run as separate programs. Install providers only from a source you trust and read the terms shown at install time. See [Provider plugins](../plugins.md).
- The paper portfolio uses simulated fills. FloweySales does not place real orders.

See also [Troubleshooting and FAQ](troubleshooting.md).
