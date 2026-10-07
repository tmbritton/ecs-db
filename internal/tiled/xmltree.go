package tiled

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// A copy-on-write XML tree: the machinery that lets Forge save a map without
// deleting the parts of it this package does not model.
//
// Nothing here knows what a map is. It is an XML document in which every
// element remembers the exact bytes it was read from, and rendering an element
// writes those bytes back — unless it or something inside it was edited, in
// which case it is written out from its attributes and children and each of
// *those* is again verbatim unless edited.
//
// So a save with no edit is byte-identical, a save with one edit differs only
// inside the smallest element containing it, and an <imagelayer>, a <group>, a
// <polygon> or an attribute Tiled has not shipped yet survives without anything
// here knowing it exists. That last property is the whole reason this is a tree
// of bytes rather than a struct: "preserve what I do not understand" is the
// default instead of a list somebody has to maintain.
//
// The alternative considered and rejected was emitting from Map. Map is a
// reading model — it keeps what the engine needs and drops nextobjectid,
// image layers, layer-folder nesting, object shapes and much else — so a writer
// built on it hands an author back a mangled file. Extending Map until it were
// lossless would be a promise re-made every time Tiled adds an element, and
// broken silently.

// xtree is a whole XML document: the prolog, whatever sits around the root, and
// the root element itself.
type xtree struct {
	nodes []xnode
	root  *xelem
}

// xnode is one thing in a document or inside an element: either an element, or
// any other token kept verbatim — whitespace, comments, character data, a
// processing instruction, a doctype. Nothing but elements is ever inspected, so
// nothing but elements needs a representation.
type xnode struct {
	raw []byte
	el  *xelem
}

// xelem is one element.
//
// src is the exact bytes it was parsed from, open tag to close tag, and dirty
// is what decides whether they are used. parent exists so marking an element
// dirty can mark its ancestors: an element whose child changed cannot be
// written from its own source bytes, because those bytes contain the old child.
type xelem struct {
	name      string
	attrs     []xattr
	kids      []xnode
	selfClose bool
	src       []byte
	dirty     bool
	parent    *xelem
}

type xattr struct {
	name  string
	value string
}

// parseTree reads a document into the tree.
//
// It refuses anything it could not write back faithfully rather than writing it
// back differently. There is one such case and it is namespaces: encoding/xml
// resolves prefixes to URLs, so a namespaced document would re-render with
// different names than it arrived with. Tiled writes none, so the refusal costs
// nothing and the alternative is a silent rewrite.
func parseTree(data []byte) (*xtree, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	tree := &xtree{}
	var stack []*xelem
	// Where each open element's tag began. Parallel to stack rather than a
	// field on xelem: it is scaffolding for parsing, and a field for it would
	// mean nothing once the tree is built.
	var openAt []int64
	// Where the next node goes: the document itself, or the element on top of
	// the stack.
	appendNode := func(n xnode) {
		if len(stack) == 0 {
			tree.nodes = append(tree.nodes, n)
			return
		}
		top := stack[len(stack)-1]
		top.kids = append(top.kids, n)
	}

	// Offsets, taken before and after each token, are what make a verbatim copy
	// possible: the token API alone cannot tell "<a/>" from "<a></a>", and a
	// re-encoder that guesses gets one of them wrong in every file.
	for {
		start := dec.InputOffset()
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tiled: this is not XML that can be edited: %w", err)
		}
		end := dec.InputOffset()

		switch t := tok.(type) {
		case xml.StartElement:
			if err := checkNoNamespace(t); err != nil {
				return nil, err
			}
			el := &xelem{name: t.Name.Local}
			for _, a := range t.Attr {
				el.attrs = append(el.attrs, xattr{name: a.Name.Local, value: a.Value})
			}
			// src is completed when the close tag arrives; until then the
			// element holds only its open tag, which is what an element that
			// never closes would render as if this returned one — it does not.
			el.src = data[start:end]
			if len(stack) > 0 {
				el.parent = stack[len(stack)-1]
			}
			appendNode(xnode{el: el})
			stack = append(stack, el)
			openAt = append(openAt, start)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("tiled: this is not XML that can be edited: unexpected </%s>", t.Name.Local)
			}
			el := stack[len(stack)-1]
			// A self-closing tag has no end tag to consume, so the decoder
			// synthesises one at the position it already stands at. A
			// zero-length range is that, and it is the only signal there is.
			el.selfClose = start == end
			el.src = data[openAt[len(openAt)-1]:end]
			stack = stack[:len(stack)-1]
			openAt = openAt[:len(openAt)-1]
		default:
			appendNode(xnode{raw: data[start:end]})
		}
	}
	// No check for elements left open. encoding/xml refuses "<map><layer>" with
	// "unexpected EOF" before the loop ends, so a guard here would be a safety
	// net over an answer already given — and reads like one that is load-bearing.
	for _, n := range tree.nodes {
		if n.el != nil {
			tree.root = n.el
			break
		}
	}
	if tree.root == nil {
		return nil, fmt.Errorf("tiled: this is not XML that can be edited: no root element")
	}
	return tree, nil
}

// xmlNamespace is the one namespace a document may use without declaring it.
const xmlNamespace = "http://www.w3.org/XML/1998/namespace"

func checkNoNamespace(t xml.StartElement) error {
	if t.Name.Space != "" {
		return fmt.Errorf("tiled: <%s> is in an XML namespace, which this editor will not rewrite; "+
			"Tiled writes no namespaces, so this file did not come from it", t.Name.Local)
	}
	for _, a := range t.Attr {
		// Three shapes, one refusal: a default declaration (xmlns=), a prefix
		// declaration (xmlns:t=), and a prefixed attribute (xml:space=), whose
		// prefix Go reports as Name.Space and which is the one that got away.
		// Only the prefix's *local* part is kept in the tree, so re-rendering
		// wrote xml:space="preserve" back as space="preserve" — and the root is
		// re-rendered by every edit, because the object counter lives on it.
		if a.Name.Space != "" || a.Name.Local == "xmlns" {
			name := a.Name.Local
			if prefix := a.Name.Space; prefix != "" {
				// Go resolves a declared prefix to its URL and leaves an
				// undeclared one as written, so this is the prefix for
				// xlink:href and a URL for xml:space. Named back for the one
				// namespace every document may use without declaring it, so the
				// message says what the file says.
				if prefix == xmlNamespace {
					prefix = "xml"
				}
				name = prefix + ":" + a.Name.Local
			}
			return fmt.Errorf("tiled: <%s> carries the XML-namespaced attribute %q, which this editor "+
				"will not rewrite; Tiled writes no namespaces, so this file did not come from it",
				t.Name.Local, name)
		}
	}
	return nil
}

func (t *xtree) render() []byte {
	var buf bytes.Buffer
	for _, n := range t.nodes {
		n.render(&buf)
	}
	return buf.Bytes()
}

func (n xnode) render(buf *bytes.Buffer) {
	if n.el != nil {
		n.el.render(buf)
		return
	}
	buf.Write(n.raw)
}

func (e *xelem) render(buf *bytes.Buffer) {
	if !e.dirty {
		buf.Write(e.src)
		return
	}
	buf.WriteByte('<')
	buf.WriteString(e.name)
	for _, a := range e.attrs {
		buf.WriteByte(' ')
		buf.WriteString(a.name)
		buf.WriteString(`="`)
		writeAttrValue(buf, a.value)
		buf.WriteByte('"')
	}
	// Self-closing only if it was, and only while it still holds nothing. The
	// children check is what decides — it is what makes putting something into
	// an element that arrived as <data/> write it out long-hand, without every
	// mutator having to remember to clear the flag.
	if e.selfClose && len(e.kids) == 0 {
		buf.WriteString("/>")
		return
	}
	buf.WriteByte('>')
	for _, k := range e.kids {
		k.render(buf)
	}
	buf.WriteString("</")
	buf.WriteString(e.name)
	buf.WriteByte('>')
}

// writeAttrValue escapes exactly what an attribute in double quotes cannot
// hold.
//
// xml.EscapeText is for character data and escapes a different set — it leaves
// the double quote alone and turns every newline into &#xA;. This writes what
// Tiled writes, which keeps a rewritten attribute looking like the ones beside
// it.
func writeAttrValue(buf *bytes.Buffer, s string) {
	for _, r := range s {
		switch r {
		case '&':
			buf.WriteString("&amp;")
		case '<':
			buf.WriteString("&lt;")
		case '>':
			buf.WriteString("&gt;")
		case '"':
			buf.WriteString("&quot;")
		case '\n':
			buf.WriteString("&#10;")
		case '\r':
			buf.WriteString("&#13;")
		case '\t':
			buf.WriteString("&#9;")
		default:
			buf.WriteRune(r)
		}
	}
}

// attrText is an attribute value escaped for a document being built as a
// string rather than as a tree.
func attrText(s string) string {
	var buf bytes.Buffer
	writeAttrValue(&buf, s)
	return buf.String()
}

// markDirty says this element can no longer be written from its source bytes,
// and neither can anything it sits inside.
//
// It stops at the first ancestor already dirty, which rests on an invariant:
// **a dirty element's ancestors are all dirty.** Every mutator here maintains
// it by going through markDirty, and newElem starts dirty with no parent and
// only ever reaches a tree through appendChild, which marks the parent. Code
// that reaches into kids directly has to call markDirty itself, and that is the
// one way to break this.
func (e *xelem) markDirty() {
	for at := e; at != nil && !at.dirty; at = at.parent {
		at.dirty = true
	}
}

func (e *xelem) attr(name string) string {
	for _, a := range e.attrs {
		if a.name == name {
			return a.value
		}
	}
	return ""
}

// setAttr replaces an attribute in place, or appends one that was not there.
//
// In place, because attribute order is how a file reads: a save that sorted
// them, or moved a changed one to the end, would produce a diff nobody can
// review over an edit that changed one character.
func (e *xelem) setAttr(name, value string) {
	for i := range e.attrs {
		if e.attrs[i].name == name {
			if e.attrs[i].value == value {
				return
			}
			e.attrs[i].value = value
			e.markDirty()
			return
		}
	}
	e.attrs = append(e.attrs, xattr{name: name, value: value})
	e.markDirty()
}

// removeAttr removes an optional attribute when an edit changes its meaning
// back to the format's default (for example an explicit int to a string).
func (e *xelem) removeAttr(name string) {
	for i, a := range e.attrs {
		if a.name == name {
			e.attrs = append(e.attrs[:i], e.attrs[i+1:]...)
			e.markDirty()
			return
		}
	}
}

func (e *xelem) firstChild(name string) *xelem {
	for _, k := range e.kids {
		if k.el != nil && k.el.name == name {
			return k.el
		}
	}
	return nil
}

func (e *xelem) children(name string) []*xelem {
	var out []*xelem
	for _, k := range e.kids {
		if k.el != nil && k.el.name == name {
			out = append(out, k.el)
		}
	}
	return out
}

// setText replaces everything inside an element with one run of character data.
//
// The text is written escaped, so a caller passes what it means rather than
// what XML needs. That is what a layer's tile data wants: commas and newlines,
// neither of which needs escaping, and no way for a caller to produce a
// document that will not parse.
func (e *xelem) setText(s string) {
	var buf bytes.Buffer
	writeText(&buf, s)
	e.kids = []xnode{{raw: buf.Bytes()}}
	e.markDirty()
}

// writeText escapes character data, and only what has to be escaped.
//
// Not xml.EscapeText, which also turns every newline into &#xA; and every tab
// into &#x9;. A layer's tile data is a grid of newline-separated rows and
// Tiled writes it as one, so escaping the newlines would turn a reviewable diff
// into a single unreadable line — for characters that are perfectly legal in
// XML content.
func writeText(buf *bytes.Buffer, s string) {
	for _, r := range s {
		switch r {
		case '&':
			buf.WriteString("&amp;")
		case '<':
			buf.WriteString("&lt;")
		case '>':
			// Legal bare, and escaped anyway: it is only illegal as part of
			// "]]>", and spotting that is more code than never writing one.
			buf.WriteString("&gt;")
		default:
			buf.WriteRune(r)
		}
	}
}

// indent is the run of spaces or tabs an element sits at, taken from the
// whitespace immediately before it.
//
// Used to write a new element in beside the ones already there. An empty answer
// means the file is not indented, and a caller that appends nothing to it
// leaves it that way — which is right: matching the file matters more than
// being pretty.
func (e *xelem) indent() string {
	if e.parent == nil {
		return ""
	}
	for i, k := range e.parent.kids {
		if k.el != e {
			continue
		}
		if i == 0 {
			return ""
		}
		return trailingIndent(string(e.parent.kids[i-1].raw))
	}
	return ""
}

// childIndent is the indentation a new child of this element should get: the
// one its existing children already use, or its own plus a step.
func (e *xelem) childIndent(step string) string {
	for _, k := range e.kids {
		if k.el != nil {
			if got := k.el.indent(); got != "" {
				return got
			}
		}
	}
	return e.indent() + step
}

func trailingIndent(s string) string {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if strings.TrimLeft(s, " \t") != "" {
		return ""
	}
	return s
}

// appendChild puts an element inside this one, after the last element already
// there and before whatever whitespace closes the parent — so a new object
// lands among the objects rather than after the indentation that was meant to
// close the group.
func (e *xelem) appendChild(child *xelem, step string) {
	child.parent = e
	lead := "\n" + e.childIndent(step)

	at := len(e.kids)
	for i := len(e.kids) - 1; i >= 0; i-- {
		if e.kids[i].el != nil {
			at = i + 1
			break
		}
		if i == 0 {
			at = 0
		}
	}
	insert := []xnode{{raw: []byte(lead)}, {el: child}}
	if len(e.kids) == 0 {
		// An element that held nothing has no closing indentation either, so
		// the close tag would end up on the same line as the child that was
		// just put in it.
		insert = append(insert, xnode{raw: []byte("\n" + e.indent())})
	}
	e.kids = append(e.kids[:at], append(insert, e.kids[at:]...)...)
	e.selfClose = false
	e.markDirty()
}

// removeChild takes an element out, along with the whitespace that indented it,
// so removing the only object from a group does not leave a blank line behind.
func (e *xelem) removeChild(child *xelem) bool {
	for i, k := range e.kids {
		if k.el != child {
			continue
		}
		from := i
		// The whitespace that indented it goes too, or removing the only
		// object from a group leaves the blank line it used to sit on.
		if i > 0 && isIndentOnly(e.kids[i-1]) {
			from = i - 1
		}
		e.kids = append(e.kids[:from], e.kids[i+1:]...)
		e.markDirty()
		return true
	}
	return false
}

// isIndentOnly reports whether a node is whitespace and nothing else, which is
// what separates an element from the one before it in an indented file.
func isIndentOnly(n xnode) bool {
	return n.el == nil && strings.TrimLeft(string(n.raw), " \t\r\n") == ""
}

// newElem builds an element that has no source bytes, so it always renders from
// its own fields.
func newElem(name string, attrs ...xattr) *xelem {
	return &xelem{name: name, attrs: attrs, selfClose: true, dirty: true}
}
