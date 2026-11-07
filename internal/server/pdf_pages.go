package server

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
)

// How a page of a PDF was classified, which decides whether the vision model
// is needed at all.
const (
	pageText    = "text"    // a text layer: read without a vision call
	pageHybrid  = "hybrid"  // a text layer plus a large picture (a chart, table or figure as an image)
	pageScanned = "scanned" // (almost) no text but a picture: a scan
	pageEmpty   = "empty"   // neither text nor a picture the server can extract
)

const (
	pdfSparseChars   = 80  // fewer characters on a page than this means no usable text layer
	pdfHybridMinPix  = 120 // a picture at least this wide and this tall on a text page is a figure candidate
	pdfMaxRawImages  = 400 // bound on the images scanned out of one file
	pdfMaxImagesPage = 3   // pictures taken from one page
)

// pdfPageInfo is what is known about one page without calling any model.
type pdfPageInfo struct {
	Number int
	Text   string
	Class  string
	Images []pdfImage
	// Note says why a page could not be given to the vision model.
	Note string
}

// pdfImage is a picture extracted from a page, ready to send to a model.
type pdfImage struct {
	Mime          string
	Data          []byte
	Width, Height int
}

// pdfDocument is a PDF split into pages.
type pdfDocument struct {
	Pages      []pdfPageInfo
	TotalPages int
	Truncated  bool // the file has more pages than were examined
}

// parsePDFPages reads the text of each page (at most readPDFMaxPages) and
// finds the pictures on pages that need or may need a vision model. The
// parser gets untrusted input, so a panic becomes an error.
func parsePDFPages(data []byte) (doc pdfDocument, err error) {
	defer func() {
		if r := recover(); r != nil {
			doc, err = pdfDocument{}, errors.New("the PDF could not be read")
		}
	}()
	if !bytes.HasPrefix(bytes.TrimLeft(data[:min(len(data), 1024)], "\x00 \t\r\n"), []byte("%PDF-")) {
		return doc, errors.New("the file is labeled as a PDF but is not one (corrupt, truncated or a blocked page)")
	}
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return doc, errors.New("the PDF could not be read")
	}
	doc.TotalPages = r.NumPage()
	doc.Truncated = doc.TotalPages > readPDFMaxPages
	var raw []rawImage
	rawScanned := false
	for i := 1; i <= doc.TotalPages && i <= readPDFMaxPages; i++ {
		page := r.Page(i)
		info := pdfPageInfo{Number: i}
		if !page.V.IsNull() {
			if t, perr := safePlainText(page); perr == nil {
				info.Text = strings.TrimSpace(crawlerSpaces.ReplaceAllString(t, " "))
			}
			refs := pageImageRefs(page)
			big := false
			for _, ref := range refs {
				if ref.W >= pdfHybridMinPix && ref.H >= pdfHybridMinPix {
					big = true
				}
			}
			switch {
			case len(info.Text) >= pdfSparseChars && big:
				info.Class = pageHybrid
			case len(info.Text) >= pdfSparseChars:
				info.Class = pageText
			case len(refs) > 0:
				info.Class = pageScanned
			default:
				info.Class = pageEmpty
			}
			if (info.Class == pageScanned || info.Class == pageHybrid) && !rawScanned {
				raw, rawScanned = scanRawImages(data), true
			}
			if info.Class == pageScanned || info.Class == pageHybrid {
				info.Images, info.Note = materializeImages(refs, raw)
			}
		} else {
			info.Class = pageEmpty
		}
		doc.Pages = append(doc.Pages, info)
	}
	return doc, nil
}

func safePlainText(p pdf.Page) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text, err = "", errors.New("unreadable page")
		}
	}()
	return p.GetPlainText(nil)
}

// imageRef describes a picture a page draws, as the PDF declares it.
type imageRef struct {
	W, H   int
	Length int64
	Filter string
	Gray   bool // DeviceGray (else DeviceRGB when Colors == 3)
	Colors int  // 1 or 3 when the colour space is simple, else 0
	Bits   int
	value  pdf.Value
}

func pageImageRefs(p pdf.Page) (refs []imageRef) {
	defer func() {
		if r := recover(); r != nil {
			refs = nil
		}
	}()
	xo := p.Resources().Key("XObject")
	names := xo.Keys()
	sort.Strings(names)
	for _, n := range names {
		v := xo.Key(n)
		if v.Key("Subtype").Name() != "Image" {
			continue
		}
		ref := imageRef{
			W: int(v.Key("Width").Int64()), H: int(v.Key("Height").Int64()),
			Length: v.Key("Length").Int64(), Bits: int(v.Key("BitsPerComponent").Int64()), value: v,
		}
		f := v.Key("Filter")
		switch f.Kind() {
		case pdf.Name:
			ref.Filter = f.Name()
		case pdf.Array:
			if f.Len() == 1 {
				ref.Filter = f.Index(0).Name()
			} else {
				ref.Filter = "multiple"
			}
		}
		switch v.Key("ColorSpace").Name() {
		case "DeviceGray":
			ref.Colors, ref.Gray = 1, true
		case "DeviceRGB":
			ref.Colors = 3
		}
		refs = append(refs, ref)
		if len(refs) >= pdfMaxImagesPage*4 {
			break
		}
	}
	return refs
}

// rawImage is a JPEG stream found in the file bytes. The PDF library cannot
// hand out DCT-encoded streams, and JPEG is how nearly every scanner and
// "print to PDF" tool stores a page, so they are read directly.
type rawImage struct {
	W, H int
	Data []byte
}

var (
	rawWidth  = regexp.MustCompile(`/Width\s+(\d+)`)
	rawHeight = regexp.MustCompile(`/Height\s+(\d+)`)
	rawLength = regexp.MustCompile(`/Length\s+(\d+)(\s+\d+\s+R)?`)
)

func scanRawImages(data []byte) (out []rawImage) {
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	pos := 0
	for len(out) < pdfMaxRawImages {
		i := bytes.Index(data[pos:], []byte("stream"))
		if i < 0 {
			break
		}
		kw := pos + i
		pos = kw + 6
		if kw >= 3 && bytes.HasPrefix(data[kw-3:], []byte("end")) {
			continue
		}
		start := kw + 6
		if start < len(data) && data[start] == '\r' {
			start++
		}
		if start < len(data) && data[start] == '\n' {
			start++
		}
		from := max(0, kw-4096)
		head := data[from:kw]
		if o := bytes.LastIndex(head, []byte(" obj")); o >= 0 {
			head = head[o:]
		}
		if !bytes.Contains(head, []byte("/Image")) || !bytes.Contains(head, []byte("DCTDecode")) {
			continue
		}
		w, h := atoiMatch(rawWidth, head), atoiMatch(rawHeight, head)
		end := bytes.Index(data[start:], []byte("endstream"))
		if end < 0 || w == 0 || h == 0 {
			continue
		}
		body := data[start : start+end]
		if m := rawLength.FindSubmatch(head); m != nil && m[2] == nil {
			if n, _ := strconv.Atoi(string(m[1])); n > 0 && n <= len(body) {
				body = body[:n]
			}
		}
		if j := bytes.LastIndex(body, []byte{0xFF, 0xD9}); j >= 0 {
			body = body[:j+2]
		}
		if len(body) < 4 || body[0] != 0xFF || body[1] != 0xD8 {
			continue
		}
		out = append(out, rawImage{W: w, H: h, Data: body})
		pos = start + end
	}
	return out
}

func atoiMatch(re *regexp.Regexp, b []byte) int {
	if m := re.FindSubmatch(b); m != nil {
		n, _ := strconv.Atoi(string(m[1]))
		return n
	}
	return 0
}

// materializeImages turns the pictures a page declares into JPEG or PNG
// bytes. What it cannot convert (fax and JBIG2 scans, unusual colour spaces,
// vector graphics) is named in the note rather than dropped silently.
func materializeImages(refs []imageRef, raw []rawImage) (imgs []pdfImage, note string) {
	var notes []string
	used := map[int]bool{}
	for _, ref := range refs {
		if len(imgs) >= pdfMaxImagesPage {
			break
		}
		if ref.W <= 0 || ref.H <= 0 {
			continue
		}
		switch ref.Filter {
		case "DCTDecode":
			found := false
			for k, ri := range raw {
				if !used[k] && ri.W == ref.W && ri.H == ref.H && (ref.Length == 0 || int64(len(ri.Data)) <= ref.Length+16) {
					used[k], found = true, true
					imgs = append(imgs, pdfImage{Mime: "image/jpeg", Data: ri.Data, Width: ref.W, Height: ref.H})
					break
				}
			}
			if !found {
				notes = append(notes, "a JPEG picture could not be located in the file")
			}
		case "FlateDecode":
			if data, ok := flateImagePNG(ref); ok {
				imgs = append(imgs, pdfImage{Mime: "image/png", Data: data, Width: ref.W, Height: ref.H})
			} else {
				notes = append(notes, "a picture uses an unsupported encoding or colour space")
			}
		default:
			notes = append(notes, "a picture uses the unsupported "+firstNonEmpty(ref.Filter, "unknown")+" encoding")
		}
	}
	return imgs, strings.Join(dedupe(notes), "; ")
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s], out = true, append(out, s)
		}
	}
	return out
}

// flateImagePNG re-encodes an 8-bit gray or RGB Flate picture as PNG.
func flateImagePNG(ref imageRef) (out []byte, ok bool) {
	defer func() {
		if recover() != nil {
			out, ok = nil, false
		}
	}()
	if ref.Bits != 8 || (ref.Colors != 1 && ref.Colors != 3) || ref.W*ref.H > 16_000_000 {
		return nil, false
	}
	want := ref.W * ref.H * ref.Colors
	buf, err := io.ReadAll(io.LimitReader(ref.value.Reader(), int64(want)+1))
	if err != nil || len(buf) != want {
		return nil, false
	}
	var img image.Image
	if ref.Colors == 1 {
		g := image.NewGray(image.Rect(0, 0, ref.W, ref.H))
		copy(g.Pix, buf)
		img = g
	} else {
		rgba := image.NewRGBA(image.Rect(0, 0, ref.W, ref.H))
		for i := 0; i < ref.W*ref.H; i++ {
			rgba.Set(i%ref.W, i/ref.W, color.RGBA{R: buf[i*3], G: buf[i*3+1], B: buf[i*3+2], A: 255})
		}
		img = rgba
	}
	var b bytes.Buffer
	if png.Encode(&b, img) != nil {
		return nil, false
	}
	return b.Bytes(), true
}
