# Email rendering

Every email FloweySales sends goes through one package, `internal/mailer`. A producer (account invitations, tracker alerts, Flow Studio Notify nodes, future features) hands over **semantic content**: a `mailer.Message` with a kind, subject, content, content type and metadata. The package renders it and sends it. No other package builds HTML, MIME headers or SMTP conversations.

A contract test (`internal/mailer/contract_test.go`) fails when any other non-test Go file under `internal/`, `cmd/` or `pluginsdk/` imports `net/smtp` or writes a raw `Subject:` header, so a new producer cannot bypass the renderer.

## What every message contains

- A branded, responsive HTML shell based on the account-invitation card: the FloweySales mark, a type label (Account, Security, Alert, Report, Flow, Notification), the title, a metadata table, the content, an optional button, and a footer. Layout is inline-styled tables, a 600 px maximum width, no external CSS, scripts, fonts or images, a hidden preheader, and `color-scheme` hints.
- A plain-text alternative generated from the same content blocks, not from the HTML. Tables become one labeled record per row, links keep their URL.
- A `multipart/alternative` MIME body: `text/plain` first, `text/html` last, both quoted-printable UTF-8, with `Date` (UTC), `Message-ID` and an RFC 2047 encoded `Subject`.
- Source and date metadata. The send time is always shown in **UTC**. Producers add labeled facts (flow, run, instrument, provider, event). Content produced by a flow or an AI is marked "Automated analysis", which keeps interpretation apart from source data; each message type adds its own notice (investment disclaimer, or for account and security messages a note that FloweySales never asks for a password or token by email).

## Content types

The producer declares how `Content` is read. The renderer never guesses.

| Type | Rendering |
| --- | --- |
| `markdown` | Headings, bold, italic, code, links, lists, quotes, rules and GitHub-style tables. Raw HTML in the source stays literal text. Underscore emphasis applies only at word boundaries, so `snake_case` and `1_000` are unchanged. |
| `json` | Parsed with the exact source spelling of numbers and the source key order. Scalar fields become key/value rows, arrays of objects tables, arrays of scalars lists, nested values sections. Null, false and empty strings stay visible. Nesting deeper than five levels is shown as compact JSON. |
| `xml` | Parsed with the standard decoder. Any `DOCTYPE` or directive, more than one root, nesting beyond 32 levels and more than 5,000 elements are refused, so entity expansion and external entities cannot occur. Attributes appear as `@name` rows, repeated record-like elements as a table. |
| `text` | Escaped paragraphs; nothing is interpreted. |
| `html` | Sanitized through an allowlist (headings, paragraphs, lists, tables, links, emphasis, code). Every attribute except a validated `href` is dropped; scripts, styles, frames, forms, comments and unknown elements are removed. |

Parsing JSON or XML that fails shows the unchanged source with a notice instead of failing the email. `Raw: true` appends the original JSON or XML after the structured view (up to 12,000 characters; a larger payload is named, with where to find it).

Flow Notify nodes use the node's declared content type (`text/markdown`, `application/json`, `application/xml`, `text/plain`, `text/html`). When none is declared, output that parses as JSON or XML is rendered as such and everything else as Markdown.

## Tables on phones

Tables with up to four columns render as tables. Wider tables render as stacked record cards: the first cell (joined with the second when the first is a rank number) is the card title and every other cell appears under its column label. Nothing needs horizontal scrolling and no cell is dropped. All text wraps (`overflow-wrap:anywhere`).

## Size limits are disclosed

Gmail clips messages near 102 KB, so the renderer bounds the HTML to 95 KB. It shows fewer rows or less source in steps (100, 40, then 12 rows) and always says what is missing: "40 of 1000 rows are shown; 960 more are omitted from this message. The complete output is in run #42 in the flow's run history." Nothing is truncated silently.

## Security

- All text is HTML-escaped. Links must be `http`, `https` or `mailto`, with no credentials, whitespace or control characters; anything else is rendered as plain text. Bare URLs are never turned into links.
- The button URL must be `http` or `https`. An unsafe URL makes rendering fail with an error that does not contain the URL or token.
- Subjects lose line breaks; recipients and senders are parsed as addresses, so headers cannot be injected.
- The preheader never carries links or tokens. Tokens are not logged.
- Rendering never calls an LLM, so figures, ranks, symbols and dates are not rewritten. Tests compare values and their order against the source.
- STARTTLS is required for every SMTP connection.

## Adding a new email

Build a `mailer.Message` and call `mailer.Send(ctx, cfg, recipient, msg)`. Pick the `Kind` that matches the message (use `KindSecurity` or `KindTransactional` for anything with a token) and declare the `ContentType`. Add a golden test if the message has a new layout (`go test ./internal/mailer -args -update` rewrites `internal/mailer/testdata/`).

## Testing

`go test ./internal/mailer` covers Markdown, JSON and XML structure, golden HTML and text for the leaderboard example and for each message kind, value and order preservation, adversarial content, safe XML parsing, size limits, MIME structure and the bypass contract. Mail-client screenshots (Gmail Android, Gmail web, Outlook, Apple Mail, dark mode) are manual checks that need real accounts; they are not part of CI.
