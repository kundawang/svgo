package svg

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// classXML is a minimal XML tree used to check generated documents.
type classXML struct {
	XMLName  xml.Name
	Attr     []xml.Attr `xml:",any,attr"`
	Children []classXML `xml:",any"`
	Text     string     `xml:",chardata"`
}

func (n *classXML) attr(name string) string {
	for _, a := range n.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func (n *classXML) all(tag string, out *[]*classXML) {
	for i := range n.Children {
		if n.Children[i].XMLName.Local == tag {
			*out = append(*out, &n.Children[i])
		}
		n.Children[i].all(tag, out)
	}
}

func (n *classXML) allTags(tag string) []*classXML {
	var out []*classXML
	n.all(tag, &out)
	return out
}

func parseClassXML(t *testing.T, doc string) *classXML {
	t.Helper()
	var root classXML
	if err := xml.Unmarshal([]byte(doc), &root); err != nil {
		t.Fatalf("output is not well-formed XML: %v\n%s", err, doc)
	}
	return &root
}

// TestStyleClassDedup checks that equal normalized styles produce exactly one
// rule and a stable name.
func TestStyleClassDedup(t *testing.T) {
	var buf bytes.Buffer
	c := New(&buf)
	c.Start(100, 100)
	a, err := c.StyleClass("fill:red;stroke:navy")
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.StyleClass("  fill : red ; stroke : navy ; ")
	if err != nil {
		t.Fatal(err)
	}
	b2, err := c.StyleClass("fill:red;stroke:navy;fill:red")
	if err != nil {
		t.Fatal(err)
	}
	if a != b || a != b2 {
		t.Fatalf("same normalized style returned different names: %q %q %q", a, b, b2)
	}
	if a != "c1" {
		t.Errorf("first automatic class = %q, want c1", a)
	}
	// declaration order is preserved in the rule (shorthand safety), so the
	// reversed style is a distinct rule even though it paints the same.
	other, err := c.StyleClass("stroke:navy;fill:red")
	if err != nil {
		t.Fatal(err)
	}
	if other == a {
		t.Fatalf("reordered declarations collapsed into one rule")
	}
	if !strings.Contains(c.ClassCSS(), ".c2{stroke:navy;fill:red}") {
		t.Errorf("declaration order not preserved: %q", c.ClassCSS())
	}
	c.Circle(5, 5, 2, ClassAttr(a))
	c.End()
	if err := xml.Unmarshal(buf.Bytes(), &classXML{}); err != nil {
		t.Fatalf("not well-formed: %v", err)
	}
	out := buf.String()
	if got := strings.Count(out, "fill:red;stroke:navy"); got != 1 {
		t.Errorf("normalized style written %d times, want 1\n%s", got, out)
	}
	if got := strings.Count(out, "<style"); got != 1 {
		t.Errorf("%d <style> elements, want 1", got)
	}
	if !strings.Contains(out, ".c1{fill:red;stroke:navy}") {
		t.Errorf("missing rule .c1{fill:red;stroke:navy}\n%s", out)
	}
}

// TestStyleClassHelpers checks the property helpers and their validation.
func TestStyleClassHelpers(t *testing.T) {
	var buf bytes.Buffer
	c := New(&buf)
	c.Start(100, 100)
	fill, err := c.FillClass("red")
	if err != nil {
		t.Fatal(err)
	}
	stroke, err := c.StrokeClass("navy")
	if err != nil {
		t.Fatal(err)
	}
	width, err := c.StrokeWidthClass(2.5)
	if err != nil {
		t.Fatal(err)
	}
	op, err := c.OpacityClass(0.5)
	if err != nil {
		t.Fatal(err)
	}
	fo, err := c.FillOpacityClass(0.25)
	if err != nil {
		t.Fatal(err)
	}
	so, err := c.StrokeOpacityClass(1)
	if err != nil {
		t.Fatal(err)
	}
	again, err := c.FillClass("red")
	if err != nil || again != fill {
		t.Errorf("FillClass(red) again = %q, %v; want %q, nil", again, err, fill)
	}
	c.Circle(5, 5, 2, ClassAttr(fill, stroke, width, op, fo, so))
	c.End()
	out := buf.String()

	for _, want := range []string{
		".c1{fill:red}",
		".c2{stroke:navy}",
		".c3{stroke-width:2.5}",
		".c4{opacity:0.5}",
		".c5{fill-opacity:0.25}",
		".c6{stroke-opacity:1}",
		`<circle cx="5" cy="5" r="2" class="c1 c2 c3 c4 c5 c6" />`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

func TestStyleClassInvalid(t *testing.T) {
	cases := []struct {
		name   string
		style  string
		target error
	}{
		{"empty", "", ErrEmptyStyle},
		{"only space", "  ", ErrEmptyStyle},
		{"only semicolons", ";;;", ErrEmptyStyle},
		{"only empty decl", "fill:", ErrInvalidStyle},
		{"no colon", "fill red", ErrInvalidStyle},
		{"empty value", "fill: ;", ErrInvalidStyle},
		{"open brace", "fill:red}", ErrInvalidStyle},
		{"close brace", "fill:red;}", ErrInvalidStyle},
		{"angle", "fill:<red>", ErrInvalidStyle},
		{"comment", "fill:red /* x */", ErrInvalidStyle},
		{"important", "fill:red !important", ErrInvalidStyle},
		{"bad property", "fi ll:red", ErrInvalidStyle},
		{"numeric property", "1fill:red", ErrInvalidStyle},
		{"custom property", "--x:red", ErrInvalidStyle},
		{"unclosed quote", `font-family:"a b`, ErrInvalidStyle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			c := New(&buf)
			c.Start(10, 10)
			name, err := c.StyleClass(tc.style)
			if !errors.Is(err, tc.target) {
				t.Fatalf("StyleClass(%q) error = %v, want %v", tc.style, err, tc.target)
			}
			if name != "" {
				t.Errorf("failed registration returned name %q, want empty", name)
			}
			c.End()
			if strings.Contains(buf.String(), "<style") {
				t.Errorf("failed registration wrote a rule:\n%s", buf.String())
			}
		})
	}
}

func TestStyleClassNames(t *testing.T) {
	var buf bytes.Buffer
	c := New(&buf)
	c.Start(10, 10)
	named, err := c.StyleClassAs("warn", "fill:orange")
	if err != nil || named != "warn" {
		t.Fatalf("StyleClassAs = %q, %v; want warn, nil", named, err)
	}
	// same style under a different explicit name reuses the first name
	again, err := c.StyleClassAs("other", "fill:orange")
	if err != nil || again != "warn" {
		t.Errorf("StyleClassAs for known style = %q, %v; want warn, nil", again, err)
	}
	// explicit name for a different style conflicts, and changes nothing
	if _, err := c.StyleClassAs("warn", "fill:red"); !errors.Is(err, ErrClassNameConflict) {
		t.Errorf("conflicting name error = %v, want ErrClassNameConflict", err)
	}
	// automatic names skip names already in use
	auto, err := c.StyleClass("fill:blue")
	if err != nil || auto != "c1" {
		t.Errorf("automatic class = %q, %v; want c1, nil", auto, err)
	}
	for _, bad := range []string{"1bad", "bad name", "bad.name", "-bad", ""} {
		if bad == "" {
			continue // empty means automatic
		}
		if _, err := c.StyleClassAs(bad, "fill:teal"); !errors.Is(err, ErrInvalidClassName) {
			t.Errorf("StyleClassAs(%q) error = %v, want ErrInvalidClassName", bad, err)
		}
	}
	c.End()
	out := buf.String()
	if got := strings.Count(out, "fill:orange"); got != 1 {
		t.Errorf("reused style written %d times, want 1\n%s", got, out)
	}
	if strings.Contains(out, "fill:red") || strings.Contains(out, "fill:teal") {
		t.Errorf("rejected registrations leaked into the stylesheet\n%s", out)
	}
}

// TestClassCascadeAndGroups checks class references on shapes, text, and
// nested groups, combined with local styles.
func TestClassCascadeAndGroups(t *testing.T) {
	var buf bytes.Buffer
	c := New(&buf)
	c.Start(200, 200)
	fill, _ := c.FillClass("red")
	stroke, _ := c.StrokeClass("navy")
	parent, _ := c.StyleClass("opacity:0.5")
	child, _ := c.StyleClass("fill:green")
	c.Circle(10, 10, 5, ClassAttr(fill, stroke), "fill:blue", `stroke-width="2"`)
	c.Text(20, 20, "hi", ClassAttr(fill), `font-size="12px"`)
	c.Group(ClassAttr(parent))
	c.Group(ClassAttr(child))
	c.Rect(1, 2, 3, 4, ClassAttr(fill, "child"))
	c.Gend()
	c.Gend()
	c.End()

	root := parseClassXML(t, buf.String())
	circles := root.allTags("circle")
	if len(circles) != 1 {
		t.Fatalf("%d circles, want 1", len(circles))
	}
	circle := circles[0]
	if cls := circle.attr("class"); cls != "c1 c2" {
		t.Errorf("circle class = %q, want %q", cls, "c1 c2")
	}
	if st := circle.attr("style"); st != "fill:blue" {
		t.Errorf("circle style = %q, want fill:blue", st)
	}
	if sw := circle.attr("stroke-width"); sw != "2" {
		t.Errorf("circle stroke-width = %q, want 2", sw)
	}
	texts := root.allTags("text")
	if len(texts) != 1 || texts[0].attr("class") != "c1" || texts[0].attr("font-size") != "12px" {
		t.Errorf("text attributes wrong: %+v", texts)
	}
	groups := root.allTags("g")
	if len(groups) != 2 {
		t.Fatalf("%d groups, want 2", len(groups))
	}
	if groups[0].attr("class") != "c3" || groups[1].attr("class") != "c4" {
		t.Errorf("nested group classes = %q, %q; want c3, c4", groups[0].attr("class"), groups[1].attr("class"))
	}
	rects := root.allTags("rect")
	if len(rects) != 1 || rects[0].attr("class") != "c1 child" {
		t.Errorf("rect class = %+v, want c1 child", rects)
	}
}

// TestClassAttrMerge checks merging of repeated class arguments and
// escaping of class names in attributes.
func TestClassAttrMerge(t *testing.T) {
	var buf bytes.Buffer
	c := New(&buf)
	c.Start(10, 10)
	c.Rect(0, 0, 1, 1, `class="a b"`, "x=1", `class="b c"`)
	c.Rect(0, 1, 1, 1, ClassAttr("a"), `class="b"`)
	c.Rect(0, 2, 1, 1)
	c.Rect(0, 3, 1, 1, ClassAttr("a&<>\"b"))
	c.End()
	out := buf.String()
	if !strings.Contains(out, `<rect x="0" y="0" width="1" height="1" class="a b c" x=1 />`) {
		t.Errorf("class arguments not merged in place\n%s", out)
	}
	if !strings.Contains(out, `<rect x="0" y="1" width="1" height="1" class="a b" />`) {
		t.Errorf("ClassAttr + hand written class not merged\n%s", out)
	}
	if !strings.Contains(out, `<rect x="0" y="2" width="1" height="1" />`) {
		t.Errorf("no class arguments must not emit an attribute\n%s", out)
	}
	if !strings.Contains(out, `class="a&amp;&lt;&gt;&#34;b"`) {
		t.Errorf("class names with XML specials not escaped\n%s", out)
	}
}

// TestStyleEscaping checks that rules with XML specials are escaped in the
// stylesheet and stable across calls.
func TestStyleEscaping(t *testing.T) {
	var buf bytes.Buffer
	c := New(&buf)
	c.Start(10, 10)
	name, err := c.StyleClass(`font-family:"A&B <Sans>";fill:red`)
	if err != nil {
		t.Fatal(err)
	}
	c.Text(1, 2, "x", ClassAttr(name))
	c.End()
	out := buf.String()
	if err := xml.Unmarshal([]byte(out), &classXML{}); err != nil {
		t.Fatalf("stylesheet broke XML: %v\n%s", err, out)
	}
	if !strings.Contains(out, `font-family:"A&amp;B &lt;Sans&gt;";fill:red`) {
		t.Errorf("rule not XML escaped\n%s", out)
	}
	if strings.Contains(out, "<Sans>") {
		t.Errorf("unescaped value in stylesheet\n%s", out)
	}
	// ClassCSS is plain CSS, unescaped
	if css := c.ClassCSS(); !strings.Contains(css, `<Sans>`) {
		t.Errorf("ClassCSS should be plain text, got %q", css)
	}
}

// TestStyleClassDocumentClosed covers registration after End and documents
// without classes.
func TestStyleClassDocumentClosed(t *testing.T) {
	var buf bytes.Buffer
	c := New(&buf)
	c.Start(10, 10)
	before, err := c.StyleClass("fill:red")
	if err != nil {
		t.Fatal(err)
	}
	c.Circle(1, 1, 1, ClassAttr(before))
	c.End()
	after, err := c.StyleClass("fill:blue")
	if !errors.Is(err, ErrDocumentClosed) {
		t.Fatalf("registration after End error = %v, want ErrDocumentClosed", err)
	}
	if after != "" {
		t.Errorf("registration after End returned name %q", after)
	}
	out := buf.String()
	if strings.Count(out, "</svg>") != 1 {
		t.Errorf("stylesheet written after </svg>\n%s", out)
	}
	if strings.Contains(out, "fill:blue") {
		t.Errorf("closed-document registration leaked\n%s", out)
	}
	if strings.Index(out, "<style") > strings.Index(out, "</svg>") {
		t.Errorf("stylesheet not inside the document\n%s", out)
	}

	// a document with no classes has exactly the historical output
	var plain bytes.Buffer
	p := New(&plain)
	p.Start(10, 10)
	p.Circle(1, 2, 3, "fill:red")
	p.Text(4, 5, "x")
	p.End()
	want := `<?xml version="1.0"?>
<!-- Generated by SVGo -->
<svg width="10" height="10"
     xmlns="http://www.w3.org/2000/svg"
     xmlns:xlink="http://www.w3.org/1999/xlink">
<circle cx="1" cy="2" r="3" style="fill:red" />
<text x="4" y="5" >x</text>
</svg>
`
	if plain.String() != want {
		t.Errorf("no-class output changed\n got:\n%s\nwant:\n%s", plain.String(), want)
	}
}

// TestStyleClassReuseAfterEnd checks that reusing an SVG after End starts a
// new document with a fresh stylesheet, while classes registered before the
// first Start are still written.
func TestStyleClassReuseAfterEnd(t *testing.T) {
	var first, second bytes.Buffer
	c := New(&first)
	c.Start(10, 10)
	c.StyleClass("fill:red")
	c.End()

	c.Writer = &second
	c.Start(10, 10)
	if css := c.ClassCSS(); css != "" {
		t.Errorf("stylesheet survived End: %q", css)
	}
	c.End()
	if strings.Contains(second.String(), "<style") {
		t.Errorf("reused document carried the old stylesheet\n%s", second.String())
	}

	var pre bytes.Buffer
	p := New(&pre)
	if _, err := p.StyleClass("fill:green"); err != nil {
		t.Fatal(err)
	}
	p.Start(10, 10)
	p.End()
	if !strings.Contains(pre.String(), ".c1{fill:green}") {
		t.Errorf("class registered before Start was lost\n%s", pre.String())
	}
}

// TestClassCSSOrder checks rule order is registration order and ClassCSS
// matches the written stylesheet.
func TestClassCSSOrder(t *testing.T) {
	var buf bytes.Buffer
	c := New(&buf)
	c.Start(10, 10)
	for _, s := range []string{"fill:red", "stroke:blue", "opacity:0.2"} {
		if _, err := c.StyleClass(s); err != nil {
			t.Fatal(err)
		}
	}
	want := ".c1{fill:red}\n.c2{stroke:blue}\n.c3{opacity:0.2}"
	if got := c.ClassCSS(); got != want {
		t.Errorf("ClassCSS = %q, want %q", got, want)
	}
	c.End()
	out := buf.String()
	pos := []int{strings.Index(out, ".c1{"), strings.Index(out, ".c2{"), strings.Index(out, ".c3{")}
	if pos[0] > pos[1] || pos[1] > pos[2] || pos[0] < 0 {
		t.Errorf("rules not in registration order: %v\n%s", pos, out)
	}
}

// TestClassFixture builds the same picture twice: once with repeated inline
// styles, once with classes, and reports the size difference. The picture has
// repeated circles, repeated text, and nested groups whose child rects take
// their paint from the containing group.
func TestClassFixture(t *testing.T) {
	const marks = 1000
	const barPaint = "fill:steelblue;fill-opacity:0.8;stroke:navy;stroke-width:1.5"
	const labelPaint = "fill:white;font-size:9px;text-anchor:middle"

	inline := func() string {
		var buf bytes.Buffer
		c := New(&buf)
		c.Start(1000, 1000)
		for i := 0; i < marks; i++ {
			x, y := 4+(i%50)*20, 4+(i/50)*20
			c.Circle(x, y, 6, barPaint)
			c.Text(x, y+3, "x", labelPaint)
		}
		for i := 0; i < 100; i++ {
			c.Gstyle(barPaint)
			c.Gtransform(fmt.Sprintf("translate(%d,10)", i*10))
			c.Rect(0, 0, 10, 10)
			c.Gend()
			c.Gend()
		}
		c.End()
		return buf.String()
	}
	classed := func() string {
		var buf bytes.Buffer
		c := New(&buf)
		c.Start(1000, 1000)
		dot, _ := c.StyleClass(barPaint)
		label, _ := c.StyleClass(labelPaint)
		for i := 0; i < marks; i++ {
			x, y := 4+(i%50)*20, 4+(i/50)*20
			c.Circle(x, y, 6, ClassAttr(dot))
			c.Text(x, y+3, "x", ClassAttr(label))
		}
		for i := 0; i < 100; i++ {
			c.Group(ClassAttr(dot))
			c.Gtransform(fmt.Sprintf("translate(%d,10)", i*10))
			c.Rect(0, 0, 10, 10)
			c.Gend()
			c.Gend()
		}
		c.End()
		return buf.String()
	}
	naive, small := inline(), classed()
	t.Logf("inline styles: %d bytes, classes: %d bytes (%.1fx smaller, %d rules)",
		len(naive), len(small), float64(len(naive))/float64(len(small)),
		strings.Count(small, "{"))
	if len(small)*2 >= len(naive) {
		t.Errorf("class document (%d bytes) is not at least 2x smaller than inline (%d bytes)",
			len(small), len(naive))
	}
	if got := strings.Count(small, "fill:steelblue"); got != 1 {
		t.Errorf("repeated paint written %d times, want 1", got)
	}
	if got := strings.Count(small, "fill:white"); got != 1 {
		t.Errorf("repeated text paint written %d times, want 1", got)
	}
	// both documents describe the same elements
	a := parseClassXML(t, naive)
	b := parseClassXML(t, small)
	for _, tag := range []string{"circle", "text", "rect", "g"} {
		if na, nb := len(a.allTags(tag)), len(b.allTags(tag)); na != nb {
			t.Errorf("%s count: inline %d, classed %d", tag, na, nb)
		}
	}
	// group-carried paint reaches the child rects without local styles
	for _, r := range b.allTags("rect") {
		if r.attr("style") != "" || r.attr("class") != "" {
			t.Errorf("rect carries local paint: %+v", r)
		}
	}
}

// TestClassAttrsCompat checks that existing style argument forms are
// unchanged, including values that merely contain "class" text.
func TestClassAttrsCompat(t *testing.T) {
	var buf bytes.Buffer
	c := New(&buf)
	c.Start(10, 10)
	c.Circle(1, 2, 3, "fill:red;opacity:0.5")
	c.Rect(1, 2, 3, 4, "classy=1")
	c.End()
	out := buf.String()
	if !strings.Contains(out, `<circle cx="1" cy="2" r="3" style="fill:red;opacity:0.5" />`) {
		t.Errorf("plain style changed\n%s", out)
	}
	if !strings.Contains(out, `classy=1`) {
		t.Errorf("attribute containing 'class' mangled\n%s", out)
	}
}
