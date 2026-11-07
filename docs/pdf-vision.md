# Reading PDFs and scanned reports (vision model)

`read_source` opens a public page for the research agent, agent teams and flow agent nodes (they all use the same tool). For PDFs it reads page by page, and where text is missing or a page carries a picture it can use a **vision model** that a SYS_ADMIN configures. The vision model is optional and independent of the chat model. Without it nothing changes except that scanned pages are reported as unread instead of silently ignored.

## What happens to a PDF

1. The file is fetched with the usual guards (public hosts, pinned address, checked redirects, 8 MB). The bytes decide the type, not the label: a generic binary type is accepted only if it starts with `%PDF-`, and a file labeled PDF that does not is refused as corrupt or a blocked page.
2. Each of the first 40 pages is classified without any model call:

   | Class | Meaning | Read by |
   | --- | --- | --- |
   | `text` | A text layer of at least 80 characters | The text layer. **No vision call is made.** |
   | `hybrid` | Text plus a large picture (a chart or table as an image) | Text layer, plus the vision model if usable |
   | `scanned` | Almost no text but a picture | The vision model, if usable |
   | `empty` | Neither text nor an extractable picture (for example vector graphics) | Nothing; reported as a limitation |

3. Text is returned as `[page N] ...` so every sentence has a page. The result contains a `document` block (per page class, method, vision state and notes, plus evidence stages and limitations) and `visual_evidence` (the vision model's observations per page). The text shown to the model also carries the **verified** vision figures as `[page N, vision] label: value unit (period)`; unverified figures are listed in `visual_evidence` only.
4. A report **viewer** page (little text, or a viewer address such as `viewer.html?file=...`) is followed to a PDF **on the same site** (same host or registrable domain), at most two candidates, through the same checks. The result names `resolved_from` and `document_url`. HTTP 403 stays a denial: it is never retried another way, the page is not reported as read, and the error says so. Cross-site PDF links are ignored.

Pictures are taken from the file: JPEG pages (what scanners and "print to PDF" produce) and 8-bit gray or RGB Flate pictures. Fax encodings (JBIG2, CCITT) and exotic colour spaces are named in the page note as unsupported. Full page rendering of vector charts is not implemented.

## Configuring the vision model (SYS_ADMIN)

Settings, API integrations, **Vision model**: endpoint URL (any OpenAI-compatible `/chat/completions` that accepts `image_url` content), model, optional fallback model on the same endpoint, an API key, "This model accepts image input", and limits. Save, then **Test with an image**: the server sends a generated red square and the model must call it red. The model is used only when it is enabled, declared image-capable **and** the test passed for the current endpoint, model and fallback. Changing any of them requires a new test. A text-only model, or an endpoint that rejects image content, fails the test with a specific reason and is never used.

The whole setting, key included, is stored as one value encrypted with `FLOWEYSALES_SECRETS_KEY`. The key is never returned by any endpoint. Pointing the setting to another endpoint without entering a key drops the old key. Users see nothing except `GET /api/vision/status` (`{"available": true|false}`).

| Request | Who | Purpose |
| --- | --- | --- |
| `GET /api/admin/vision` | SYS_ADMIN | Settings without the key, status, limits, telemetry |
| `PUT /api/admin/vision` | SYS_ADMIN | Save (blank `api_key` keeps the key; `clear_key` removes it) |
| `POST /api/admin/vision/test` | SYS_ADMIN | Run the image test and record the result |
| `DELETE /api/admin/vision` | SYS_ADMIN | Remove settings and key |
| `GET /api/vision/status` | any signed-in user | Whether scanned documents can be read |

Changes are written to the flow audit log (request hash only, no content).

## Limits, cost and cache

| Limit | Default | Maximum |
| --- | --- | --- |
| Pages sent per document | 6 | 20 |
| Images per run | 20 | 100 |
| Tokens per run | 200,000 | 2,000,000 |
| Image size / pixels | 4 MB / 16 MP | 8 MB / 40 MP |
| Output tokens per call | 3,000 | 8,192 |
| Timeout per call | 90 s | 300 s |
| Parallel calls | 2 | 4 |

The per-run caps are shared by every document and every member of a team run. Scanned pages go first, then hybrid pages. A page over a cap is marked `skipped` or `failed` with the reason. Cancelling the run cancels pending vision calls. If an endpoint reports no token usage, the run says that the token cap cannot be enforced for those calls.

Results are cached in memory for 12 hours by user, model, extraction schema version and image hash; one user's result is never served to another, and the cache is cleared when the endpoint or model changes. Nothing from a document is written to disk, logged or stored in telemetry (the admin card shows only counts and the last error code and time, UTC).

## What the vision model may and may not do

The model only transcribes: each observation has the page, an optional region, group and role (component or total), label, the **value exactly as printed**, unit, currency, period as printed and its kind (annual, half, quarter, month, point in time), a source excerpt and the model's own uncertainty. The page number is set by the server, not the model. Page images are treated as data; instructions printed in them are ignored.

Code then checks every observation: the value is parsed deterministically (US, Swedish and European formats, parentheses and minus signs; ambiguous forms such as `1,234` are refused, never guessed), the sign comes from the printed value, the printed value must appear in the quoted source text, a period must be stated, a figure labeled a quarter whose period is an annual one is flagged (no quarterly figure is invented from annual aggregates), and, where the model marked components and a total, the sum is compared with the total. Only observations that pass are `verified`. "Verified" means these automatic checks; nothing is confirmed against an independent data source.

## Evidence panel

In the tool-call view of `read_source`, **Document evidence** shows the document link (and the viewer page it came from), pages examined, per page the class, how it was read (text layer, vision model, both, not read) and vision state, a table of figures with page, printed value, period and checks (flags in plain words), and the limitations. Its summary line keeps apart: PDF retrieved, text parsed, pages interpreted by the vision model, and values that passed the automatic checks.

## Not yet implemented

- A dedicated extract-table / extract-figure tool and a document preview with figure thumbnails.
- Rendering vector pages to images (charts drawn as vectors are reported as unread); an optional local OCR fallback.
- A persistent, cross-run source cache and benchmarks against real Swedish and US reports.
- Region (bounding box) overlays; the box is stored when the model gives one.
- Per-user or per-team opt-out of sending document images to the configured endpoint (today the administrator's decision applies to every run).


### Input integrity and incomplete responses

The document reader now rejects HTML exceeding its 512 KB fetch limit, instead of silently treating a partial page as a complete source. It also rejects responses labeled PDF when their content lacks a PDF file signature (e.g. an HTML error page mislabeled as `application/pdf`). Large PDFs remain subject to the 8 MB limit. These are **errors**, not evidence: the agent must not cite an unread or truncated page as an extracted document. HTTP 403 restrictions remain unchanged.


### Usage safety

If a multimodal provider omits token usage or sends invalid counters, document analysis does **not** assume the call was free: further vision image reservations are denied for that run to avoid uncontrolled costs. Negative image reservations/releases are ignored. Administrators should review incomplete extraction evidence and configure appropriate run budgets.
