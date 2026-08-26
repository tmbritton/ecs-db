package tiled

import (
	"os"
	"strings"
	"testing"
)

// The tree's whole promise is that a document it did not edit comes back
// exactly as it arrived. These are the shapes that break naive re-encoders.
func TestParseTree_RendersBackByteIdentical(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"a prolog and a root", "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<map/>\n"},
		{"self-closing children", "<map>\n <tileset firstgid=\"1\"/>\n <layer/>\n</map>"},
		{"an empty element written long-hand", "<map><layer></layer></map>"},
		{"comments outside and inside", "<!-- before -->\n<map>\n <!-- inside -->\n</map>\n<!-- after -->"},
		{"character data", "<map><data>1,2,3\n4,5,6\n</data></map>"},
		{"escaped text", "<map><data>a &amp; b &lt; c</data></map>"},
		{"escaped attributes", `<map name="a &amp; b" desc="&quot;q&quot;"/>`},
		{"a CDATA section", "<map><data><![CDATA[1,2,3]]></data></map>"},
		{"a doctype", "<!DOCTYPE map>\n<map/>"},
		{"tabs and mixed indentation", "<map>\n\t<layer>\n\t\t<data>1</data>\n\t</layer>\n</map>"},
		{"no trailing newline", "<map/>"},
		{"trailing whitespace after the root", "<map/>\n\n  \n"},
		{"a processing instruction after the prolog", "<?xml version=\"1.0\"?>\n<?tiled hint?>\n<map/>"},
		{"attributes in an order nobody would sort", `<map z="1" a="2" m="3"/>`},
		// The four below are the ones that make the verbatim path load-bearing:
		// each is a spelling this package's own renderer would not reproduce, so
		// a tree that re-rendered every element would change them all.
		{"single-quoted attributes", `<map a='one' b='two'/>`},
		{"generous whitespace inside a tag", "<map  a=\"1\"\n     b=\"2\" >x</map>"},
		{"numeric character references in an attribute", `<map a="&#65;&#x42;"/>`},
		{"a character reference in text", "<map><d>&#65;&amp;B</d></map>"},
		{"CRLF line endings", "<?xml version=\"1.0\"?>\r\n<map>\r\n <layer/>\r\n</map>\r\n"},
		{"a UTF-8 byte-order mark", "\ufeff<?xml version=\"1.0\"?>\n<map/>\n"},
		{"the shipped level", ""}, // filled below
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.src
			if src == "" {
				raw, err := os.ReadFile("../../mods/map/level1.tmx")
				if err != nil {
					t.Fatal(err)
				}
				src = string(raw)
			}
			tree, err := parseTree([]byte(src))
			if err != nil {
				t.Fatalf("parseTree: %v", err)
			}
			if got := string(tree.render()); got != src {
				t.Errorf("round trip changed the document\n got: %q\nwant: %q", got, src)
			}
		})
	}
}

func TestParseTree_RefusesWhatItCannotRenderBack(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"not XML at all", "hello", "XML"},
		{"an unclosed element", "<map><layer></map>", "layer"},
		{"no root element", "<?xml version=\"1.0\"?>\n", "no root element"},
		{"a namespace declaration", `<map xmlns="http://x"/>`, "namespace"},
		{"a namespaced element", `<t:map xmlns:t="http://x"/>`, "namespace"},
		// Without the declaration, so the element check is what has to refuse it
		// — with one, the xmlns attribute is caught first and the element check
		// never runs.
		{"an element with an undeclared prefix", `<map><t:layer/></map>`, "<layer>"},
		// The one that got away in review: only the local part of a prefixed
		// attribute is kept, so re-rendering turned xml:space="preserve" into
		// space="preserve" — on the root, which every edit re-renders.
		{"a prefixed attribute", `<map xml:space="preserve"/>`, "xml:space"},
		{"an attribute with an undeclared prefix", `<map xlink:href="a"/>`, "xlink:href"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseTree([]byte(tc.src))
			if err == nil {
				t.Fatal("expected a refusal")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refusal %q does not mention %q", err, tc.want)
			}
		})
	}
}

// The point of the copy-on-write tree: an edit re-renders the elements that
// contain it and copies every other byte through untouched.
func TestTree_AnEditRewritesOnlyWhatContainsIt(t *testing.T) {
	const src = `<map version="1.10" nextobjectid="3">
 <layer id="1" name="ground">
  <data encoding="csv">1,2,3</data>
 </layer>
 <imagelayer id="4" name="sky" tint="#ff0000">
  <image source="sky.png" width="64" height="64"/>
 </imagelayer>
 <objectgroup id="2" name="spawns">
  <object id="1" type="Goblin" x="32" y="64">
   <polygon points="0,0 8,0 8,8"/>
  </object>
 </objectgroup>
</map>
`
	tree, err := parseTree([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	layer := tree.root.firstChild("layer")
	if layer == nil {
		t.Fatal("no layer element")
	}
	layer.setAttr("name", "floor")

	got := string(tree.render())
	want := strings.Replace(src, `name="ground"`, `name="floor"`, 1)
	if got != want {
		t.Errorf("an attribute edit changed more than the attribute\n got: %q\nwant: %q", got, want)
	}
}

func TestTree_ADirtyElementKeepsItsCleanChildrenVerbatim(t *testing.T) {
	const src = "<map a=\"1\">\n <objectgroup>\n  <object id=\"1\">\n   <polygon points=\"0,0 8,8\"/>\n  </object>\n </objectgroup>\n</map>"
	tree, err := parseTree([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	tree.root.setAttr("a", "2")
	got := string(tree.render())
	if !strings.Contains(got, `<polygon points="0,0 8,8"/>`) {
		t.Errorf("a self-closing grandchild was rewritten: %q", got)
	}
	if got != strings.Replace(src, `a="1"`, `a="2"`, 1) {
		t.Errorf("got %q", got)
	}
}

func TestTree_SetAttrAddsOneThatWasNotThere(t *testing.T) {
	tree, err := parseTree([]byte(`<map version="1.10"/>`))
	if err != nil {
		t.Fatal(err)
	}
	tree.root.setAttr("nextobjectid", "7")
	if got, want := string(tree.render()), `<map version="1.10" nextobjectid="7"/>`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTree_ARewrittenAttributeIsEscaped(t *testing.T) {
	tree, err := parseTree([]byte(`<map name="x"/>`))
	if err != nil {
		t.Fatal(err)
	}
	tree.root.setAttr("name", "a & b <c> \"d\"\ne")
	got := string(tree.render())
	// Re-read it: whatever the escaping is, it has to survive a parse.
	back, err := parseTree([]byte(got))
	if err != nil {
		t.Fatalf("the escaped attribute will not parse: %v (%q)", err, got)
	}
	if v := back.root.attr("name"); v != "a & b <c> \"d\"\ne" {
		t.Errorf("attribute did not survive escaping: %q", v)
	}
}

func TestTree_ChildrenAndLookup(t *testing.T) {
	tree, err := parseTree([]byte(`<map><layer id="1"/><layer id="2"/><objectgroup id="3"/></map>`))
	if err != nil {
		t.Fatal(err)
	}
	layers := tree.root.children("layer")
	if len(layers) != 2 {
		t.Fatalf("got %d layers, want 2", len(layers))
	}
	if layers[1].attr("id") != "2" {
		t.Errorf("children are out of document order: %q", layers[1].attr("id"))
	}
	if tree.root.firstChild("objectgroup").attr("id") != "3" {
		t.Error("firstChild found the wrong element")
	}
	if tree.root.firstChild("nothing") != nil {
		t.Error("firstChild invented an element")
	}
}

func TestTree_AppendChildLandsAmongTheChildren(t *testing.T) {
	const src = "<map>\n <objectgroup>\n  <object id=\"1\"/>\n </objectgroup>\n</map>\n"
	tree, err := parseTree([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	group := tree.root.firstChild("objectgroup")
	group.appendChild(newElem("object", xattr{"id", "2"}), " ")

	want := "<map>\n <objectgroup>\n  <object id=\"1\"/>\n  <object id=\"2\"/>\n </objectgroup>\n</map>\n"
	if got := string(tree.render()); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestTree_AppendChildIndentsAStepInWhenThereAreNoChildren(t *testing.T) {
	const src = "<map>\n <objectgroup/>\n</map>\n"
	tree, err := parseTree([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	tree.root.firstChild("objectgroup").appendChild(newElem("object", xattr{"id", "1"}), " ")

	want := "<map>\n <objectgroup>\n  <object id=\"1\"/>\n </objectgroup>\n</map>\n"
	if got := string(tree.render()); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestTree_RemoveChildTakesItsIndentationWithIt(t *testing.T) {
	const src = "<map>\n <objectgroup>\n  <object id=\"1\"/>\n  <object id=\"2\"/>\n </objectgroup>\n</map>\n"
	tree, err := parseTree([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	group := tree.root.firstChild("objectgroup")
	target := group.children("object")[0]
	if !group.removeChild(target) {
		t.Fatal("removeChild reported it removed nothing")
	}
	want := "<map>\n <objectgroup>\n  <object id=\"2\"/>\n </objectgroup>\n</map>\n"
	if got := string(tree.render()); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if group.removeChild(target) {
		t.Error("removeChild removed an element that was already gone")
	}
}

func TestTree_SetTextReplacesEverythingInside(t *testing.T) {
	tree, err := parseTree([]byte("<map>\n <data encoding=\"csv\">1,2\n3,4\n</data>\n</map>"))
	if err != nil {
		t.Fatal(err)
	}
	tree.root.firstChild("data").setText("\n9,9,\n9,9\n")
	want := "<map>\n <data encoding=\"csv\">\n9,9,\n9,9\n</data>\n</map>"
	if got := string(tree.render()); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestTree_SetTextOnASelfClosingElementOpensIt(t *testing.T) {
	tree, err := parseTree([]byte(`<map><data/></map>`))
	if err != nil {
		t.Fatal(err)
	}
	tree.root.firstChild("data").setText("1,2")
	if got, want := string(tree.render()), `<map><data>1,2</data></map>`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTree_SetTextEscapesWhatXMLCannotHold(t *testing.T) {
	tree, err := parseTree([]byte(`<map><d/></map>`))
	if err != nil {
		t.Fatal(err)
	}
	tree.root.firstChild("d").setText("a < b & c")
	back, err := parseTree(tree.render())
	if err != nil {
		t.Fatalf("the escaped text will not parse: %v (%q)", err, tree.render())
	}
	_ = back
	if strings.Contains(string(tree.render()), "a < b") {
		t.Errorf("text was written unescaped: %q", tree.render())
	}
}

func TestTree_SettingAnAttributeToWhatItAlreadyIsChangesNothing(t *testing.T) {
	// The document stays byte-identical, which is what keeps "dirty" a
	// comparison the save footer can trust.
	const src = "<map version=\"1.10\">\n <layer id=\"1\"/>\n</map>\n"
	tree, err := parseTree([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	tree.root.setAttr("version", "1.10")
	if tree.root.dirty {
		t.Error("a no-op set marked the root dirty")
	}
	if got := string(tree.render()); got != src {
		t.Errorf("got %q, want %q", got, src)
	}
}

// Together with the round-trip cases above, this is what makes selfClose mean
// something: an element written long-hand and then edited stays long-hand.
// Collapsing it to <a/> loses nothing a parser would notice and changes a file
// somebody has to review.
func TestTree_AnEmptyElementWrittenLongHandStaysLongHand(t *testing.T) {
	tree, err := parseTree([]byte(`<map><objectgroup id="1"></objectgroup></map>`))
	if err != nil {
		t.Fatal(err)
	}
	group := tree.root.firstChild("objectgroup")
	group.setAttr("id", "2")
	if got, want := string(tree.render()), `<map><objectgroup id="2"></objectgroup></map>`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTree_AnEditedSelfClosingElementStaysSelfClosing(t *testing.T) {
	tree, err := parseTree([]byte(`<map><objectgroup id="1"/></map>`))
	if err != nil {
		t.Fatal(err)
	}
	tree.root.firstChild("objectgroup").setAttr("id", "2")
	if got, want := string(tree.render()), `<map><objectgroup id="2"/></map>`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
