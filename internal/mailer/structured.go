package mailer

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	maxStructuredBytes = 2 << 20
	maxJSONDepth       = 64
	maxXMLDepth        = 32
	maxXMLNodes        = 5000
	displayDepth       = 5
	maxColumns         = 30
)

// limits bound how much of a structured payload is shown in the message body.
type limits struct {
	rows  int // rows of one table or items of one list
	chars int // characters of Markdown, text and HTML source
}

// ---------- JSON ----------

type jnode struct {
	kind  byte // o object, a array, s string, n number, b bool, z null
	str   string
	keys  []string
	items []*jnode // values of an object or elements of an array
}

func parseJSONValue(dec *json.Decoder, depth int) (*jnode, error) {
	if depth > maxJSONDepth {
		return nil, errors.New("json nested too deeply")
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch v := tok.(type) {
	case json.Delim:
		if v == '{' {
			n := &jnode{kind: 'o'}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := kt.(string)
				val, err := parseJSONValue(dec, depth+1)
				if err != nil {
					return nil, err
				}
				n.keys = append(n.keys, key)
				n.items = append(n.items, val)
			}
			_, err := dec.Token()
			return n, err
		}
		n := &jnode{kind: 'a'}
		for dec.More() {
			val, err := parseJSONValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			n.items = append(n.items, val)
		}
		_, err := dec.Token()
		return n, err
	case string:
		return &jnode{kind: 's', str: v}, nil
	case json.Number:
		return &jnode{kind: 'n', str: v.String()}, nil
	case bool:
		return &jnode{kind: 'b', str: strconv.FormatBool(v)}, nil
	case nil:
		return &jnode{kind: 'z', str: "null"}, nil
	}
	return nil, errors.New("unexpected json token")
}

func parseJSON(src string) (*jnode, error) {
	if len(src) > maxStructuredBytes {
		return nil, errors.New("payload too large")
	}
	dec := json.NewDecoder(strings.NewReader(src))
	dec.UseNumber() // numbers keep their exact source spelling
	n, err := parseJSONValue(dec, 0)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after json value")
	}
	return n, nil
}

func (n *jnode) scalar() bool { return n.kind != 'o' && n.kind != 'a' }

// scalarText shows a scalar exactly; an empty string is shown as "" so it is not
// mistaken for a missing value, and null stays null.
func (n *jnode) scalarText() string {
	switch n.kind {
	case 's':
		if n.str == "" {
			return `""`
		}
		return n.str
	default:
		return n.str
	}
}

func (n *jnode) compact() string {
	var sb strings.Builder
	n.writeCompact(&sb)
	return sb.String()
}

func (n *jnode) writeCompact(sb *strings.Builder) {
	switch n.kind {
	case 's':
		b, _ := json.Marshal(n.str)
		sb.Write(b)
	case 'o':
		sb.WriteByte('{')
		for i, k := range n.keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			sb.Write(kb)
			sb.WriteByte(':')
			n.items[i].writeCompact(sb)
		}
		sb.WriteByte('}')
	case 'a':
		sb.WriteByte('[')
		for i, it := range n.items {
			if i > 0 {
				sb.WriteByte(',')
			}
			it.writeCompact(sb)
		}
		sb.WriteByte(']')
	default:
		sb.WriteString(n.str)
	}
}

func (n *jnode) cell() []Inline {
	if n.scalar() {
		return plain(n.scalarText())
	}
	return []Inline{{Kind: 'c', Text: n.compact()}}
}

func omitted(shown, total int, what string) Block {
	return Note{Text: fmt.Sprintf("%d of %d %s are shown; %d more are omitted from this message.", shown, total, what, total-shown)}
}

// JSONBlocks renders a JSON document as readable blocks. Values, units, nulls,
// number spelling and key order are kept as they appear in the source.
func JSONBlocks(src string, lim limits) ([]Block, error) {
	root, err := parseJSON(src)
	if err != nil {
		return nil, err
	}
	var out []Block
	jsonRender(&out, "", root, 2, 0, lim)
	return out, nil
}

func headingFor(level int, label string) Block {
	if level > 4 {
		level = 4
	}
	return Heading{Level: level, Text: plain(label)}
}

func jsonRender(out *[]Block, label string, n *jnode, level, depth int, lim limits) {
	if label != "" {
		*out = append(*out, headingFor(level, label))
	}
	if depth >= displayDepth && !n.scalar() {
		*out = append(*out, Code{Text: n.compact()})
		return
	}
	switch n.kind {
	case 'o':
		var kv KV
		for i, k := range n.keys {
			if n.items[i].scalar() {
				kv.Rows = append(kv.Rows, KVRow{Key: k, Value: plain(n.items[i].scalarText())})
			}
		}
		if len(kv.Rows) > lim.rows {
			total := len(kv.Rows)
			kv.Rows = kv.Rows[:lim.rows]
			*out = append(*out, kv, omitted(lim.rows, total, "fields"))
		} else if len(kv.Rows) > 0 {
			*out = append(*out, kv)
		}
		for i, k := range n.keys {
			if !n.items[i].scalar() {
				jsonRender(out, k, n.items[i], level+1, depth+1, lim)
			}
		}
	case 'a':
		jsonArray(out, n, level, depth, lim)
	default:
		*out = append(*out, Para{Text: plain(n.scalarText())})
	}
}

func jsonArray(out *[]Block, n *jnode, level, depth int, lim limits) {
	total := len(n.items)
	if total == 0 {
		*out = append(*out, Note{Text: "This list is empty."})
		return
	}
	shown := n.items
	if len(shown) > lim.rows {
		shown = shown[:lim.rows]
	}
	allObjects, allScalars := true, true
	for _, it := range shown {
		if it.kind != 'o' {
			allObjects = false
		}
		if !it.scalar() {
			allScalars = false
		}
	}
	switch {
	case allObjects:
		var cols []string
		seen := map[string]int{}
		for _, it := range shown {
			for _, k := range it.keys {
				if _, ok := seen[k]; !ok && len(cols) < maxColumns {
					seen[k] = len(cols)
					cols = append(cols, k)
				}
			}
		}
		tb := Table{}
		for _, c := range cols {
			tb.Header = append(tb.Header, plain(c))
		}
		for _, it := range shown {
			row := make([][]Inline, len(cols))
			for i, k := range it.keys {
				if idx, ok := seen[k]; ok && row[idx] == nil {
					row[idx] = it.items[i].cell()
				}
			}
			tb.Rows = append(tb.Rows, row)
		}
		*out = append(*out, tb)
		if len(seen) >= maxColumns {
			*out = append(*out, Note{Text: fmt.Sprintf("Only the first %d fields are shown as columns.", maxColumns)})
		}
	case allScalars:
		list := List{}
		for _, it := range shown {
			list.Items = append(list.Items, ListItem{Text: plain(it.scalarText())})
		}
		*out = append(*out, list)
	default:
		for i, it := range shown {
			jsonRender(out, fmt.Sprintf("Item %d", i+1), it, level+1, depth+1, lim)
		}
	}
	if total > len(shown) {
		*out = append(*out, omitted(len(shown), total, "items"))
	}
}

// ---------- XML ----------

type xnode struct {
	name  string
	attrs [][2]string
	text  string
	kids  []*xnode
}

func parseXML(src string) (*xnode, error) {
	if len(src) > maxStructuredBytes {
		return nil, errors.New("payload too large")
	}
	// Go's decoder never resolves external entities or fetches DTDs; any DOCTYPE
	// or other directive is refused anyway, so entity expansion tricks cannot
	// reach the renderer.
	dec := xml.NewDecoder(strings.NewReader(src))
	dec.Strict = true
	var stack []*xnode
	var root *xnode
	nodes := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.Directive:
			return nil, errors.New("xml directives are not allowed")
		case xml.StartElement:
			nodes++
			if nodes > maxXMLNodes || len(stack) >= maxXMLDepth {
				return nil, errors.New("xml is too large or nested too deeply")
			}
			n := &xnode{name: t.Name.Local}
			for _, a := range t.Attr {
				n.attrs = append(n.attrs, [2]string{a.Name.Local, a.Value})
			}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.kids = append(p.kids, n)
			} else if root != nil {
				return nil, errors.New("xml has more than one root element")
			} else {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.text += string(t)
			}
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, errors.New("xml has no complete root element")
	}
	return root, nil
}

func (n *xnode) leaf() bool { return len(n.kids) == 0 && len(n.attrs) == 0 }

func (n *xnode) record() bool {
	for _, k := range n.kids {
		if !k.leaf() {
			return false
		}
	}
	return true
}

// XMLBlocks renders an XML document as structured sections: attributes and
// text-only children become key/value rows, repeated record-like elements a table.
func XMLBlocks(src string, lim limits) ([]Block, error) {
	root, err := parseXML(src)
	if err != nil {
		return nil, err
	}
	var out []Block
	xmlRender(&out, root, 2, 0, lim)
	return out, nil
}

func xmlRender(out *[]Block, n *xnode, level, depth int, lim limits) {
	*out = append(*out, headingFor(level, n.name))
	var kv KV
	for _, a := range n.attrs {
		kv.Rows = append(kv.Rows, KVRow{Key: "@" + a[0], Value: plain(a[1])})
	}
	if t := strings.TrimSpace(n.text); t != "" {
		kv.Rows = append(kv.Rows, KVRow{Key: "(text)", Value: plain(t)})
	}
	for _, k := range n.kids {
		if k.leaf() {
			kv.Rows = append(kv.Rows, KVRow{Key: k.name, Value: plain(strings.TrimSpace(k.text))})
		}
	}
	if len(kv.Rows) > lim.rows {
		total := len(kv.Rows)
		kv.Rows = kv.Rows[:lim.rows]
		*out = append(*out, kv, omitted(lim.rows, total, "fields"))
	} else if len(kv.Rows) > 0 {
		*out = append(*out, kv)
	}
	for i := 0; i < len(n.kids); {
		k := n.kids[i]
		if k.leaf() {
			i++
			continue
		}
		j := i
		for j < len(n.kids) && n.kids[j].name == k.name && !n.kids[j].leaf() {
			j++
		}
		group := n.kids[i:j]
		i = j
		if depth+1 >= displayDepth {
			for _, g := range group {
				*out = append(*out, headingFor(level+1, g.name), Code{Text: xmlCompact(g)})
			}
			continue
		}
		if len(group) >= 2 && allRecords(group) {
			xmlTable(out, k.name, group, level+1, lim)
			continue
		}
		for _, g := range group {
			xmlRender(out, g, level+1, depth+1, lim)
		}
	}
}

func allRecords(g []*xnode) bool {
	for _, n := range g {
		if !n.record() || strings.TrimSpace(n.text) != "" {
			return false
		}
	}
	return true
}

func xmlTable(out *[]Block, name string, group []*xnode, level int, lim limits) {
	*out = append(*out, headingFor(level, name))
	var cols []string
	seen := map[string]int{}
	add := func(c string) {
		if _, ok := seen[c]; !ok && len(cols) < maxColumns {
			seen[c] = len(cols)
			cols = append(cols, c)
		}
	}
	shown := group
	if len(shown) > lim.rows {
		shown = shown[:lim.rows]
	}
	for _, g := range shown {
		for _, a := range g.attrs {
			add("@" + a[0])
		}
		for _, k := range g.kids {
			add(k.name)
		}
	}
	tb := Table{}
	for _, c := range cols {
		tb.Header = append(tb.Header, plain(c))
	}
	for _, g := range shown {
		row := make([][]Inline, len(cols))
		for _, a := range g.attrs {
			if idx, ok := seen["@"+a[0]]; ok {
				row[idx] = plain(a[1])
			}
		}
		for _, k := range g.kids {
			if idx, ok := seen[k.name]; ok && row[idx] == nil {
				row[idx] = plain(strings.TrimSpace(k.text))
			}
		}
		tb.Rows = append(tb.Rows, row)
	}
	*out = append(*out, tb)
	if len(group) > len(shown) {
		*out = append(*out, omitted(len(shown), len(group), "elements"))
	}
}

func xmlCompact(n *xnode) string {
	var sb strings.Builder
	sb.WriteString("<" + n.name)
	for _, a := range n.attrs {
		sb.WriteString(" " + a[0] + "=" + strconv.Quote(a[1]))
	}
	sb.WriteString(">")
	sb.WriteString(strings.TrimSpace(n.text))
	for _, k := range n.kids {
		sb.WriteString(xmlCompact(k))
	}
	sb.WriteString("</" + n.name + ">")
	return sb.String()
}
