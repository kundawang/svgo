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

// TestNestedSymbolFixture checks the XML structure, output order, single
// geometry emission and instance bounds of the two-level nested fixture,
// using floating point coordinates.
func TestNestedSymbolFixture(t *testing.T) {
	const instances = 1000
	var buf bytes.Buffer
	canvas := New(&buf)
	canvas.Start(1400, 900)
	canvas.Def()
	// level 0: the base icon geometry, written once
	canvas.Symbol("map-pin", 0, 0, 24, 24)
	canvas.Path(pinPath, "fill:crimson")
	canvas.Circle(12, 9, 3, "fill:white")
	canvas.SymbolEnd()
	// level 1: a badge that nests the base icon
	canvas.Symbol("pin-marker", 0, 0, 32, 32)
	canvas.UseSymbol(4.5, 4.5, "map-pin", 1)
	canvas.Circle(26, 6, 5, "fill:navy")
	canvas.SymbolEnd()
	canvas.DefEnd()
	// instances of the two-level icon
	type placement struct{ x, y, s float64 }
	var placed []placement
	for i := 0; i < instances; i++ {
		x := 10.5 + float64(i%40)*34.25
		y := 20.5 + float64(i/40)*34.25
		s := 1.0
		if i%3 == 0 {
			s = 2.5
		}
		canvas.UseSymbol(x, y, "pin-marker", s)
		placed = append(placed, placement{x, y, s})
	}
	canvas.End()
	if err := canvas.ValidateSymbols(); err != nil {
		t.Fatalf("ValidateSymbols: %v", err)
	}
	out := buf.String()

	// well-formed XML
	var root xmlnode
	if err := xml.Unmarshal([]byte(out), &root); err != nil {
		t.Fatalf("output is not well-formed XML: %v", err)
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
	if vb := pin.attr("viewBox"); vb != "0.00 0.00 24.00 24.00" {
		t.Errorf("map-pin viewBox = %q, want %q", vb, "0.00 0.00 24.00 24.00")
	}
	nested := marker.find("use")
	if nested == nil || nested.attr("href") != "#map-pin" {
		t.Error("pin-marker must nest a <use> of #map-pin")
	}
	if got := root.count("use"); got != instances {
		t.Fatalf("document has %d top-level <use> elements, want %d", got, instances)
	}

	// instances come after the definitions
	defsEnd := strings.Index(out, "</defs>")
	if defsEnd < 0 || strings.Index(out[defsEnd:], "<use ") < 0 {
		t.Fatal("no instance <use> after </defs>")
	}

	// geometry written once
	if got := strings.Count(out, pinPath); got != 1 {
		t.Errorf("icon geometry appears %d times, want 1", got)
	}

	// instance bounds and transforms
	for _, i := range []int{0, 1, 333, 999} {
		p := placed[i]
		bx, by, bw, bh, err := canvas.SymbolBounds("pin-marker", p.x, p.y, p.s)
		if err != nil {
			t.Fatalf("SymbolBounds: %v", err)
		}
		if bx != p.x || by != p.y || bw != 32*p.s || bh != 32*p.s {
			t.Errorf("instance %d bounds = (%v,%v,%v,%v), want (%v,%v,%v,%v)",
				i, bx, by, bw, bh, p.x, p.y, 32*p.s, 32*p.s)
		}
		want := fmt.Sprintf(`transform="translate(%.2f,%.2f)"`, p.x, p.y)
		if p.s != 1 {
			want = fmt.Sprintf(`transform="translate(%.2f,%.2f) scale(%g)"`, p.x, p.y, p.s)
		}
		if !strings.Contains(out, want) {
			t.Errorf("instance %d: output missing %s", i, want)
		}
	}
	// nested symbol bounds: map-pin placed at (4.5,4.5) scale 1 inside pin-marker
	if bx, by, bw, bh, err := canvas.SymbolBounds("map-pin", 4.5, 4.5, 1); err != nil ||
		bx != 4.5 || by != 4.5 || bw != 24 || bh != 24 {
		t.Errorf("nested bounds = (%v,%v,%v,%v), err=%v; want (4.5,4.5,24,24)", bx, by, bw, bh, err)
	}
}

// TestSymbolFileSize compares the symbol document against a naive document
// that writes the full icon geometry for every instance.
func TestSymbolFileSize(t *testing.T) {
	const instances = 1000
	var symBuf bytes.Buffer
	sym := New(&symBuf)
	sym.Start(1400, 900)
	sym.Def()
	sym.Symbol("map-pin", 0, 0, 24, 24)
	sym.Path(pinPath, "fill:crimson")
	sym.Circle(12, 9, 3, "fill:white")
	sym.SymbolEnd()
	sym.Symbol("pin-marker", 0, 0, 32, 32)
	sym.UseSymbol(4.5, 4.5, "map-pin", 1)
	sym.Circle(26, 6, 5, "fill:navy")
	sym.SymbolEnd()
	sym.DefEnd()
	for i := 0; i < instances; i++ {
		sym.UseSymbol(10.5+float64(i%40)*34.25, 20.5+float64(i/40)*34.25, "pin-marker", 1)
	}
	sym.End()

	var naive bytes.Buffer
	c := New(&naive)
	c.Start(1400, 900)
	for i := 0; i < instances; i++ {
		x := 10.5 + float64(i%40)*34.25
		y := 20.5 + float64(i/40)*34.25
		c.Gtransform(fmt.Sprintf("translate(%.2f,%.2f)", x, y))
		c.Path(pinPath, "fill:crimson")
		c.Circle(12, 9, 3, "fill:white")
		c.Circle(26, 6, 5, "fill:navy")
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

// TestSymbolErrors checks that every misuse produces a locatable error.
func TestSymbolErrors(t *testing.T) {
	cases := []struct {
		name string
		run  func(c *SVG) error
		want []string
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
			c.UseSymbol(5.5, 6.5, "ghost", 1)
			return c.ValidateSymbols()
		}, []string{"ghost", "undefined", "(5.5,6.5)"}},
		{"circular reference", func(c *SVG) error {
			c.Symbol("a", 0, 0, 8, 8)
			c.UseSymbol(0, 0, "b", 1)
			c.SymbolEnd()
			c.Symbol("b", 0, 0, 8, 8)
			c.UseSymbol(0, 0, "a", 1)
			c.SymbolEnd()
			return c.ValidateSymbols()
		}, []string{"circular", "a", "b"}},
		{"invalid viewBox", func(c *SVG) error {
			err := c.Symbol("flat", 0, 0, 24, -1)
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
			c.UseSymbol(3, 4, "pin", math.Inf(-1))
			return c.ValidateSymbols()
		}, []string{"pin", "non-finite", "(1,2)", "(3,4)"}},
		{"unclosed definition", func(c *SVG) error {
			c.Symbol("dangling", 0, 0, 8, 8)
			return c.ValidateSymbols()
		}, []string{"dangling", "unclosed"}},
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

// TestForwardReference checks that a symbol may be used before it is defined.
func TestForwardReference(t *testing.T) {
	var buf bytes.Buffer
	canvas := New(&buf)
	canvas.Start(100, 100)
	canvas.UseSymbol(10.5, 10.5, "later", 1.5)
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
	if !strings.Contains(buf.String(), `transform="translate(10.50,10.50) scale(1.5)"`) {
		t.Error("missing scaled instance transform")
	}
}

// TestExistingUseCompat verifies the pre-existing Use method and basic
// shapes are unchanged by the symbol support.
func TestExistingUseCompat(t *testing.T) {
	var buf bytes.Buffer
	canvas := New(&buf)
	canvas.Start(100, 100)
	canvas.Use(10.5, 20.5, "#thing")
	canvas.Circle(5.5, 6.5, 3.5, "fill:red")
	canvas.End()
	out := buf.String()
	for _, want := range []string{
		`<use x="10.50" y="20.50" xlink:href="#thing" />`,
		`<circle cx="5.50" cy="6.50" r="3.50" style="fill:red" />`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, out)
		}
	}
	if err := canvas.ValidateSymbols(); err != nil {
		t.Errorf("plain Use must not create symbol errors: %v", err)
	}
}
