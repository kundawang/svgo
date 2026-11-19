package svg

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"math"
	"strings"
	"testing"
)

// pinPath is the geometry of the map-pin icon. In symbol output it must
// appear exactly once, no matter how many times the icon is instantiated.
const pinPath = "M12 2C8.1 2 5 5.1 5 9c0 5.2 7 13 7 13s7-7.8 7-13c0-3.9-3.1-7-7-7z"

// xmlnode is a generic XML tree for structural checks.
type xmlnode struct {
	XMLName  xml.Name
	Attr     []xml.Attr `xml:",any,attr"`
	Children []xmlnode  `xml:",any"`
}

func (n *xmlnode) attr(name string) string {
	for _, a := range n.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func (n *xmlnode) find(tag string) *xmlnode {
	for i := range n.Children {
		if n.Children[i].XMLName.Local == tag {
			return &n.Children[i]
		}
	}
	return nil
}

func (n *xmlnode) count(tag string) int {
	c := 0
	for _, ch := range n.Children {
		if ch.XMLName.Local == tag {
			c++
		}
	}
	return c
}

// legendFixture builds the two-level nested icon fixture: symbol "pin-marker"
// (level 1) references symbol "map-pin" (level 0), and "pin-marker" is then
// instantiated n times. It returns the document and the instance placements.
func legendFixture(canvas *SVG, n int) (xs, ys []int, scales []float64) {
	canvas.Start(1400, 900)
	canvas.Def()
	// level 0: the base icon geometry, written once
	canvas.Symbol("map-pin", 0, 0, 24, 24)
	canvas.Path(pinPath, "fill:crimson")
	canvas.Circle(12, 9, 3, "fill:white")
	canvas.SymbolEnd()
	// level 1: a badge that nests the base icon
	canvas.Symbol("pin-marker", 0, 0, 32, 32)
	canvas.UseSymbol(4, 4, "map-pin", 1)
	canvas.Circle(26, 6, 5, "fill:navy")
	canvas.SymbolEnd()
	canvas.DefEnd()
	// instances of the two-level icon
	for i := 0; i < n; i++ {
		x := 10 + (i%40)*34
		y := 20 + (i/40)*34
		s := 1.0
		if i%3 == 0 {
			s = 2.0
		}
		canvas.UseSymbol(x, y, "pin-marker", s)
		xs = append(xs, x)
		ys = append(ys, y)
		scales = append(scales, s)
	}
	canvas.End()
	return xs, ys, scales
}

// TestNestedSymbolFixture checks the XML structure, output order, single
// geometry emission and instance bounds of the two-level nested fixture.
func TestNestedSymbolFixture(t *testing.T) {
	const instances = 1000
	var buf bytes.Buffer
	canvas := New(&buf)
	xs, ys, scales := legendFixture(canvas, instances)
	if err := canvas.ValidateSymbols(); err != nil {
		t.Fatalf("ValidateSymbols: %v", err)
	}
	out := buf.String()

	// well-formed XML
	var root xmlnode
	if err := xml.Unmarshal([]byte(out), &root); err != nil {
		t.Fatalf("output is not well-formed XML: %v", err)
	}
	if root.XMLName.Local != "svg" {
		t.Fatalf("root element = %q, want svg", root.XMLName.Local)
	}

	// structure: defs holds both symbols; level-1 symbol nests a use of level 0
	defs := root.find("defs")
	if defs == nil {
		t.Fatal("no <defs> element")
	}
	if got := defs.count("symbol"); got != 2 {
		t.Fatalf("defs contains %d symbols, want 2", got)
	}
	var pin, marker *xmlnode
	for i := range defs.Children {
		s := &defs.Children[i]
		switch s.attr("id") {
		case "map-pin":
			pin = s
		case "pin-marker":
			marker = s
		}
	}
	if pin == nil || marker == nil {
		t.Fatal("defs must contain symbols map-pin and pin-marker")
	}
	if vb := pin.attr("viewBox"); vb != "0 0 24 24" {
		t.Errorf("map-pin viewBox = %q, want %q", vb, "0 0 24 24")
	}
	if vb := marker.attr("viewBox"); vb != "0 0 32 32" {
		t.Errorf("pin-marker viewBox = %q, want %q", vb, "0 0 32 32")
	}
	if pin.find("path") == nil {
		t.Error("map-pin must contain the icon path")
	}
	nested := marker.find("use")
	if nested == nil || nested.attr("href") != "#map-pin" {
		t.Error("pin-marker must nest a <use> of #map-pin")
	}

	// the instances: 1000 top-level uses, all after the defs block
	if got := root.count("use"); got != instances {
		t.Fatalf("document has %d top-level <use> elements, want %d", got, instances)
	}
	defsEnd := strings.Index(out, "</defs>")
	firstInstance := strings.Index(out[defsEnd:], "<use ")
	if firstInstance < 0 {
		t.Fatal("no instance <use> after </defs>")
	}

	// geometry written once
	if got := strings.Count(out, pinPath); got != 1 {
		t.Errorf("icon geometry appears %d times, want 1", got)
	}

	// instance bounds and transforms
	for _, i := range []int{0, 1, 333, 999} {
		x, y, s := xs[i], ys[i], scales[i]
		bx, by, bw, bh, err := canvas.SymbolBounds("pin-marker", x, y, s)
		if err != nil {
			t.Fatalf("SymbolBounds: %v", err)
		}
		if bx != float64(x) || by != float64(y) || bw != 32*s || bh != 32*s {
			t.Errorf("instance %d bounds = (%v,%v,%v,%v), want (%v,%v,%v,%v)",
				i, bx, by, bw, bh, float64(x), float64(y), 32*s, 32*s)
		}
		want := fmt.Sprintf(`transform="translate(%d,%d)"`, x, y)
		if s != 1 {
			want = fmt.Sprintf(`transform="translate(%d,%d) scale(%g)"`, x, y, s)
		}
		if !strings.Contains(out, want) {
			t.Errorf("instance %d: output missing %s", i, want)
		}
	}
	// nested symbol bounds: map-pin placed at (4,4) scale 1 inside pin-marker
	if bx, by, bw, bh, err := canvas.SymbolBounds("map-pin", 4, 4, 1); err != nil ||
		bx != 4 || by != 4 || bw != 24 || bh != 24 {
		t.Errorf("nested bounds = (%v,%v,%v,%v), err=%v; want (4,4,24,24)", bx, by, bw, bh, err)
	}
	if _, _, _, _, err := canvas.SymbolBounds("no-such", 0, 0, 1); err == nil {
		t.Error("SymbolBounds for undefined symbol should fail")
	}
}

// TestSymbolFileSize compares the symbol fixture against a naive document
// that writes the full icon geometry for every instance.
func TestSymbolFileSize(t *testing.T) {
	const instances = 1000
	var symBuf bytes.Buffer
	legendFixture(New(&symBuf), instances)

	var naive bytes.Buffer
	c := New(&naive)
	c.Start(1400, 900)
	for i := 0; i < instances; i++ {
		x := 10 + (i%40)*34
		y := 20 + (i/40)*34
		s := 1.0
		if i%3 == 0 {
			s = 2.0
		}
		c.Gtransform(fmt.Sprintf("translate(%d,%d) scale(%g)", x, y, s))
		c.Path(pinPath, "fill:crimson") // level 0 geometry, repeated
		c.Circle(12, 9, 3, "fill:white")
		c.Circle(26, 6, 5, "fill:navy") // level 1 badge, repeated
		c.Gend()
	}
	c.End()

	symSize, naiveSize := symBuf.Len(), naive.Len()
	t.Logf("symbol document: %d bytes, naive document: %d bytes (%.1fx smaller)",
		symSize, naiveSize, float64(naiveSize)/float64(symSize))
	if symSize*3 >= naiveSize {
		t.Errorf("symbol document (%d bytes) is not at least 3x smaller than naive (%d bytes)",
			symSize, naiveSize)
	}
}

// TestForwardReference checks that a symbol may be used before it is defined.
func TestForwardReference(t *testing.T) {
	var buf bytes.Buffer
	canvas := New(&buf)
	canvas.Start(100, 100)
	canvas.UseSymbol(10, 10, "later", 1.5) // referenced before definition
	canvas.Def()
	canvas.Symbol("later", 0, 0, 8, 8)
	canvas.Rect(0, 0, 8, 8)
	canvas.SymbolEnd()
	canvas.DefEnd()
	canvas.End()
	if err := canvas.ValidateSymbols(); err != nil {
		t.Fatalf("forward reference should validate: %v", err)
	}
	var root xmlnode
	if err := xml.Unmarshal(buf.Bytes(), &root); err != nil {
		t.Fatalf("output is not well-formed XML: %v", err)
	}
	if !strings.Contains(buf.String(), `transform="translate(10,10) scale(1.5)"`) {
		t.Error("missing scaled instance transform")
	}
}

// TestSymbolErrors checks that every misuse produces a locatable error.
func TestSymbolErrors(t *testing.T) {
	cases := []struct {
		name string
		run  func(c *SVG) error
		want []string // substrings required in the error
	}{
		{"duplicate id", func(c *SVG) error {
			c.Symbol("pin", 0, 0, 24, 24)
			c.SymbolEnd()
			err := c.Symbol("pin", 0, 0, 24, 24)
			c.SymbolEnd()
			if err == nil {
				t.Fatal("duplicate definition not reported at call time")
			}
			return c.ValidateSymbols()
		}, []string{"pin", "duplicate", "op #"}},
		{"missing reference", func(c *SVG) error {
			c.UseSymbol(5, 6, "ghost", 1)
			return c.ValidateSymbols()
		}, []string{"ghost", "undefined", "(5,6)"}},
		{"circular reference", func(c *SVG) error {
			c.Symbol("a", 0, 0, 8, 8)
			c.UseSymbol(0, 0, "b", 1)
			c.SymbolEnd()
			c.Symbol("b", 0, 0, 8, 8)
			c.UseSymbol(0, 0, "a", 1)
			c.SymbolEnd()
			return c.ValidateSymbols()
		}, []string{"circular", "a", "b"}},
		{"self reference", func(c *SVG) error {
			c.Symbol("loop", 0, 0, 8, 8)
			c.UseSymbol(0, 0, "loop", 1)
			c.SymbolEnd()
			return c.ValidateSymbols()
		}, []string{"circular", "loop"}},
		{"invalid viewBox", func(c *SVG) error {
			err := c.Symbol("flat", 0, 0, 0, 24)
			c.SymbolEnd()
			if err == nil {
				t.Fatal("invalid viewBox not reported at call time")
			}
			return c.ValidateSymbols()
		}, []string{"flat", "viewBox"}},
		{"non-finite scale", func(c *SVG) error {
			c.Symbol("pin", 0, 0, 24, 24)
			c.SymbolEnd()
			err := c.UseSymbol(1, 2, "pin", math.NaN())
			if err == nil {
				t.Fatal("NaN scale not reported at call time")
			}
			c.UseSymbol(3, 4, "pin", math.Inf(1))
			return c.ValidateSymbols()
		}, []string{"pin", "non-finite", "(1,2)", "(3,4)"}},
		{"unclosed definition", func(c *SVG) error {
			c.Symbol("dangling", 0, 0, 8, 8)
			return c.ValidateSymbols()
		}, []string{"dangling", "unclosed"}},
		{"end without begin", func(c *SVG) error {
			err := c.SymbolEnd()
			if err == nil {
				t.Fatal("SymbolEnd without Symbol not reported at call time")
			}
			return c.ValidateSymbols()
		}, []string{"no open symbol"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			canvas := New(&buf)
			canvas.Start(100, 100)
			err := tc.run(canvas)
			canvas.End()
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q missing %q", err.Error(), want)
				}
			}
		})
	}
}

// TestExistingUseCompat verifies the pre-existing Use method and basic
// shapes are unchanged by the symbol support.
func TestExistingUseCompat(t *testing.T) {
	var buf bytes.Buffer
	canvas := New(&buf)
	canvas.Start(100, 100)
	canvas.Use(10, 20, "#thing")
	canvas.Circle(5, 6, 3, "fill:red")
	canvas.Rect(1, 2, 3, 4)
	canvas.Text(7, 8, "hi")
	canvas.End()
	out := buf.String()
	for _, want := range []string{
		`<use x="10" y="20" xlink:href="#thing" />`,
		`<circle cx="5" cy="6" r="3" style="fill:red" />`,
		`<rect x="1" y="2" width="3" height="4" />`,
		`<text x="7" y="8" >hi</text>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, out)
		}
	}
	if err := canvas.ValidateSymbols(); err != nil {
		t.Errorf("plain Use must not create symbol errors: %v", err)
	}
}
