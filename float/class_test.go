package svg

import (
	"bytes"
	"encoding/xml"
	"errors"
	"strings"
	"testing"
)

// TestFloatStyleClass checks the class API in the floating point package:
// deduplicated rules, class references on shapes, text and nested groups,
// local override, and the stylesheet placement.
func TestFloatStyleClass(t *testing.T) {
	var buf bytes.Buffer
	c := New(&buf)
	c.Start(200, 200)
	fill, err := c.FillClass("red")
	if err != nil {
		t.Fatal(err)
	}
	again, err := c.StyleClass("fill:red")
	if err != nil || again != fill {
		t.Fatalf("StyleClass(fill:red) = %q, %v; want %q", again, err, fill)
	}
	op, err := c.OpacityClass(0.4)
	if err != nil {
		t.Fatal(err)
	}
	sw, err := c.StrokeWidthClass(0.75)
	if err != nil {
		t.Fatal(err)
	}
	c.Circle(10.5, 10.5, 3.25, ClassAttr(fill, op), "fill:blue")
	c.Group(ClassAttr(fill))
	c.Text(1, 2, "hi", ClassAttr(sw))
	c.Gend()
	c.End()

	out := buf.String()
	if err := xml.Unmarshal([]byte(out), &struct{}{}); err != nil {
		t.Fatalf("not well-formed: %v\n%s", err, out)
	}
	for _, want := range []string{
		".c1{fill:red}",
		".c2{opacity:0.4}",
		".c3{stroke-width:0.75}",
		`<circle cx="10.50" cy="10.50" r="3.25" class="c1 c2" style="fill:blue" />`,
		`<g class="c1" >`,
		`<text x="1.00" y="2.00" class="c3" >hi</text>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
	if got := strings.Count(out, "fill:red"); got != 1 {
		t.Errorf("fill:red written %d times, want 1", got)
	}
	if strings.Index(out, "<style") > strings.Index(out, "</svg>") || !strings.Contains(out, "</style>\n</svg>") {
		t.Errorf("stylesheet not written inside the document\n%s", out)
	}
}

// TestFloatStyleErrors checks the validation failures and closed-document
// behavior.
func TestFloatStyleErrors(t *testing.T) {
	var buf bytes.Buffer
	c := New(&buf)
	c.Start(10, 10)
	cases := []struct {
		name string
		call func() (string, error)
		want error
	}{
		{"empty", func() (string, error) { return c.StyleClass(" ") }, ErrEmptyStyle},
		{"invalid", func() (string, error) { return c.StyleClass("fill:red}") }, ErrInvalidStyle},
		{"bad name", func() (string, error) { return c.StyleClassAs("9x", "fill:red") }, ErrInvalidClassName},
		{"nan opacity", func() (string, error) { return c.OpacityClass(1.5) }, ErrInvalidStyle},
		{"zero width", func() (string, error) { return c.StrokeWidthClass(0) }, ErrInvalidStyle},
	}
	for _, tc := range cases {
		if _, err := tc.call(); !errors.Is(err, tc.want) {
			t.Errorf("%s: error = %v, want %v", tc.name, err, tc.want)
		}
	}
	c.End()
	if _, err := c.StyleClass("fill:red"); !errors.Is(err, ErrDocumentClosed) {
		t.Errorf("post-End error = %v, want ErrDocumentClosed", err)
	}

	var plain bytes.Buffer
	p := New(&plain)
	p.Start(1, 1)
	p.End()
	if strings.Contains(plain.String(), "<style") {
		t.Errorf("no-class document gained a stylesheet\n%s", plain.String())
	}
}
