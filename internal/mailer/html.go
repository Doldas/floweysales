package mailer

import (
	"fmt"
	"strings"
)

// Design tokens adapted from the account invitation card, the visual reference.
const (
	colBg      = "#0b1020"
	colCard    = "#151d32"
	colSurface = "#1e293b"
	colBorder  = "#334155"
	colText    = "#e2e8f0"
	colBody    = "#cbd5e1"
	colMuted   = "#94a3b8"
	colAccent  = "#a5b4fc"
	colLink    = "#67e8f9"
	colButton  = "#5d60ec" // white text on it is 4.8:1 (WCAG AA needs 4.5:1)
	fontStack  = "Arial,Helvetica,sans-serif"
	monoStack  = "Consolas,Menlo,monospace"
	wrapSafe   = "overflow-wrap:anywhere;word-break:break-word;"
)

var tagStyles = map[string]string{
	"p":          "margin:0 0 14px;color:" + colBody + ";font-size:16px;line-height:1.6;" + wrapSafe,
	"h1":         "margin:22px 0 10px;color:#ffffff;font-size:22px;line-height:1.3;",
	"h2":         "margin:22px 0 10px;color:#ffffff;font-size:20px;line-height:1.3;",
	"h3":         "margin:20px 0 8px;color:#ffffff;font-size:17px;line-height:1.3;",
	"h4":         "margin:18px 0 8px;color:" + colAccent + ";font-size:15px;line-height:1.3;",
	"h5":         "margin:16px 0 6px;color:" + colAccent + ";font-size:14px;",
	"h6":         "margin:16px 0 6px;color:" + colAccent + ";font-size:14px;",
	"ul":         "margin:0 0 14px;padding-left:22px;color:" + colBody + ";font-size:16px;line-height:1.6;",
	"ol":         "margin:0 0 14px;padding-left:22px;color:" + colBody + ";font-size:16px;line-height:1.6;",
	"li":         "margin:0 0 6px;" + wrapSafe,
	"blockquote": "margin:0 0 14px;padding:4px 14px;border-left:3px solid " + colAccent + ";color:" + colMuted + ";",
	"pre":        "margin:0 0 14px;padding:12px;background:" + colBg + ";border:1px solid " + colBorder + ";border-radius:8px;color:" + colText + ";font-family:" + monoStack + ";font-size:13px;line-height:1.5;white-space:pre-wrap;" + wrapSafe,
	"code":       "background:" + colBg + ";color:" + colText + ";font-family:" + monoStack + ";font-size:14px;padding:1px 4px;border-radius:4px;" + wrapSafe,
	"a":          "color:" + colLink + ";text-decoration:underline;" + wrapSafe,
	"hr":         "border:0;border-top:1px solid " + colBorder + ";margin:22px 0;",
	"table":      "width:100%;border-collapse:collapse;margin:0 0 14px;font-size:14px;",
	"th":         "padding:8px;border:1px solid " + colBorder + ";background:" + colSurface + ";color:" + colAccent + ";text-align:left;vertical-align:top;" + wrapSafe,
	"td":         "padding:8px;border:1px solid " + colBorder + ";color:" + colText + ";vertical-align:top;" + wrapSafe,
}

func openTag(name, href string) string {
	style := tagStyles[name]
	switch name {
	case "br":
		return "<br>"
	case "hr":
		return `<hr style="` + style + `">`
	case "a":
		if href == "" {
			return `<a style="` + style + `">`
		}
		return `<a href="` + esc(href) + `" rel="noopener noreferrer nofollow" style="` + style + `">`
	case "b", "strong", "i", "em", "u", "thead", "tbody", "tr":
		return "<" + name + ">"
	}
	return "<" + name + ` style="` + style + `">`
}

func inlinesHTML(in []Inline) string {
	var sb strings.Builder
	for _, x := range in {
		switch x.Kind {
		case 't':
			sb.WriteString(strings.ReplaceAll(esc(x.Text), "\n", "<br>"))
		case 'b':
			sb.WriteString("<strong style=\"color:#ffffff\">" + inlinesHTML(x.Children) + "</strong>")
		case 'i':
			sb.WriteString("<em>" + inlinesHTML(x.Children) + "</em>")
		case 'c':
			sb.WriteString(openTag("code", "") + esc(x.Text) + "</code>")
		case 'l':
			sb.WriteString(openTag("a", x.URL) + inlinesHTML(x.Children) + "</a>")
		}
	}
	return sb.String()
}

func blocksHTML(blocks []Block) string {
	var sb strings.Builder
	for _, b := range blocks {
		switch v := b.(type) {
		case Heading:
			// The message title is the only h1; headings in the content start at h2.
			level := v.Level + 1
			if level < 2 {
				level = 2
			}
			if level > 5 {
				level = 5
			}
			tag := fmt.Sprintf("h%d", level)
			sb.WriteString(openTag(tag, "") + inlinesHTML(v.Text) + "</" + tag + ">")
		case Para:
			sb.WriteString(openTag("p", "") + inlinesHTML(v.Text) + "</p>")
		case List:
			tag := "ul"
			if v.Ordered {
				tag = "ol"
			}
			sb.WriteString(openTag(tag, ""))
			for _, it := range v.Items {
				style := tagStyles["li"]
				if it.Indent > 0 {
					style += fmt.Sprintf("margin-left:%dpx;", it.Indent*18)
				}
				sb.WriteString(`<li style="` + style + `">` + inlinesHTML(it.Text) + "</li>")
			}
			sb.WriteString("</" + tag + ">")
		case Code:
			sb.WriteString(openTag("pre", "") + esc(v.Text) + "</pre>")
		case Quote:
			sb.WriteString(openTag("blockquote", "") + blocksHTML(v.Blocks) + "</blockquote>")
		case Rule:
			sb.WriteString(openTag("hr", ""))
		case Table:
			sb.WriteString(tableHTML(v))
		case KV:
			sb.WriteString(kvHTML(v))
		case Note:
			sb.WriteString(`<p style="margin:0 0 14px;padding:8px 12px;border:1px dashed ` + colBorder + `;border-radius:8px;color:` + colMuted + `;font-size:13px;line-height:1.5;` + wrapSafe + `">` + esc(v.Text) + "</p>")
		case RawHTML:
			sb.WriteString(v.HTML)
		}
	}
	return sb.String()
}

func kvHTML(v KV) string {
	var sb strings.Builder
	sb.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="` + tagStyles["table"] + `">`)
	for _, r := range v.Rows {
		sb.WriteString("<tr>" +
			`<td width="35%" style="padding:6px 8px 6px 0;border-bottom:1px solid ` + colBorder + `;color:` + colMuted + `;font-size:13px;vertical-align:top;` + wrapSafe + `">` + esc(r.Key) + "</td>" +
			`<td style="padding:6px 0;border-bottom:1px solid ` + colBorder + `;color:` + colText + `;font-size:14px;vertical-align:top;` + wrapSafe + `">` + inlinesHTML(r.Value) + "</td></tr>")
	}
	sb.WriteString("</table>")
	return sb.String()
}

// wideTable is the column count above which a table becomes stacked cards so
// nothing scrolls sideways on a phone.
const wideTable = 4

func tableHTML(t Table) string {
	cols := len(t.Header)
	for _, r := range t.Rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols > wideTable {
		return cardsHTML(t, cols)
	}
	var sb strings.Builder
	sb.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="` + tagStyles["table"] + `">`)
	if len(t.Header) > 0 {
		sb.WriteString("<thead><tr>")
		for _, h := range t.Header {
			sb.WriteString(openTag("th", "") + inlinesHTML(h) + "</th>")
		}
		sb.WriteString("</tr></thead>")
	}
	sb.WriteString("<tbody>")
	for _, r := range t.Rows {
		sb.WriteString("<tr>")
		for k := 0; k < cols; k++ {
			var cell []Inline
			if k < len(r) {
				cell = r[k]
			}
			sb.WriteString(openTag("td", "") + inlinesHTML(cell) + "</td>")
		}
		sb.WriteString("</tr>")
	}
	sb.WriteString("</tbody></table>")
	return sb.String()
}

// cardTitle picks the heading of a record card: the first cell, joined with the
// second when the first is a short rank-like value.
func cardTitle(t Table, row [][]Inline) (title []Inline, skip int) {
	if len(row) == 0 {
		return nil, 0
	}
	first := flatText(row[0])
	if len(row) > 1 && len(first) <= 3 && isRankLike(first) {
		title = append(title, row[0]...)
		title = append(title, Inline{Kind: 't', Text: ". "})
		title = append(title, row[1]...)
		return title, 2
	}
	return row[0], 1
}

func isRankLike(s string) bool {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "#"), "."))
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func cardsHTML(t Table, cols int) string {
	var sb strings.Builder
	for _, row := range t.Rows {
		title, skip := cardTitle(t, row)
		sb.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="width:100%;margin:0 0 12px;background:` + colSurface + `;border:1px solid ` + colBorder + `;border-radius:10px"><tr><td style="padding:12px 14px;` + wrapSafe + `">`)
		sb.WriteString(`<div style="font-size:16px;font-weight:bold;color:#ffffff;margin:0 0 6px;` + wrapSafe + `">` + inlinesHTML(title) + `</div>`)
		for k := skip; k < cols; k++ {
			label := ""
			if k < len(t.Header) {
				label = flatText(t.Header[k])
			}
			var cell []Inline
			if k < len(row) {
				cell = row[k]
			}
			sb.WriteString(`<div style="font-size:14px;line-height:1.5;color:` + colText + `;` + wrapSafe + `"><span style="color:` + colMuted + `;font-size:12px">` + esc(label) + `</span><br>` + inlinesHTML(cell) + `</div>`)
		}
		sb.WriteString("</td></tr></table>")
	}
	return sb.String()
}

func flatText(in []Inline) string {
	var sb strings.Builder
	for _, x := range in {
		switch x.Kind {
		case 't', 'c':
			sb.WriteString(x.Text)
		case 'l':
			sb.WriteString(flatText(x.Children))
			sb.WriteString(" (" + x.URL + ")")
		default:
			sb.WriteString(flatText(x.Children))
		}
	}
	return sb.String()
}

func shellHTML(m Message, blocks, extra []Block) string {
	var sb strings.Builder
	pre := m.Preheader
	if pre == "" {
		pre = m.Title
	}
	sb.WriteString(`<!doctype html><html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="dark light"><meta name="supported-color-schemes" content="dark light"><title>` + esc(m.Subject) + `</title>`)
	sb.WriteString(`<style>@media (max-width:480px){.fs-card{padding:20px 16px!important}.fs-outer{padding:12px 6px!important}}</style></head>`)
	sb.WriteString(`<body style="margin:0;padding:0;background:` + colBg + `;font-family:` + fontStack + `;color:` + colText + `">`)
	sb.WriteString(`<div style="display:none;max-height:0;overflow:hidden;opacity:0;mso-hide:all;font-size:1px;line-height:1px;color:` + colBg + `">` + esc(pre) + strings.Repeat("&zwnj;&nbsp;", 30) + `</div>`)
	sb.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:` + colBg + `"><tr><td align="center" class="fs-outer" style="padding:32px 12px">`)
	sb.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="width:100%;max-width:600px;background:` + colCard + `;border:1px solid ` + colBorder + `;border-radius:16px"><tr><td class="fs-card" style="padding:32px">`)
	sb.WriteString(`<p style="font-size:19px;font-weight:bold;color:` + colAccent + `;margin:0 0 4px">✦ FloweySales</p>`)
	sb.WriteString(`<p style="font-size:11px;letter-spacing:1px;text-transform:uppercase;color:` + colMuted + `;margin:0 0 24px">` + esc(m.kindLabel()) + `</p>`)
	sb.WriteString(`<h1 style="font-size:26px;line-height:1.25;color:#ffffff;margin:0 0 14px;` + wrapSafe + `">` + esc(m.Title) + `</h1>`)
	sb.WriteString(kvHTML(KV{Rows: metaRows(m)}))
	sb.WriteString(blocksHTML(blocks))
	if m.Action != nil {
		sb.WriteString(`<p style="margin:30px 0"><a href="` + esc(m.Action.URL) + `" style="display:inline-block;padding:14px 24px;background:` + colButton + `;color:#ffffff;text-decoration:none;font-weight:bold;border-radius:9px">` + esc(m.Action.Label) + `</a></p>`)
		if m.ActionNote != "" {
			sb.WriteString(`<p style="font-size:13px;color:` + colAccent + `;margin:0 0 12px">` + esc(m.ActionNote) + `</p>`)
		}
		sb.WriteString(`<p style="font-size:13px;color:` + colMuted + `;line-height:1.6;margin:0">If the button doesn't work, copy this link:<br><a href="` + esc(m.Action.URL) + `" style="color:` + colLink + `;` + wrapSafe + `">` + esc(m.Action.URL) + `</a></p>`)
	}
	sb.WriteString(blocksHTML(extra))
	sb.WriteString(`<hr style="border:0;border-top:1px solid ` + colBorder + `;margin:26px 0">`)
	for _, f := range m.footer() {
		sb.WriteString(`<p style="font-size:12px;color:` + colMuted + `;line-height:1.6;margin:0 0 8px;` + wrapSafe + `">` + esc(f) + `</p>`)
	}
	sb.WriteString(`</td></tr></table></td></tr></table></body></html>`)
	return sb.String()
}

func metaRows(m Message) []KVRow {
	var rows []KVRow
	for _, r := range m.metaRows() {
		rows = append(rows, KVRow{Key: r.Label, Value: plain(r.Value)})
	}
	return rows
}
