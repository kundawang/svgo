package svg

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"
)

// autoDoc renders d in automatic viewBox mode and returns the output and canvas.
func autoDoc(margin float64, w, h int, d func(*SVG)) (string, *SVG) {
	var buf bytes.Buffer
	c := NewAutoViewBox(&buf, margin)
	c.Start(w, h)
	d(c)
	c.End()
	return buf.String(), c
}

// wantViewBox asserts the output contains exactly the expected viewBox.
func wantViewBox(t *testing.T, out, vb string) {
	t.Helper()
	if !strings.Contains(out, `viewBox="`+vb+`"`) {
		t.Errorf("viewBox %q not found in:\n%s", vb, out)
	}
}

func TestAutoViewBoxRotatedRect(t *testing.T) {
	out, c := autoDoc(0, 200, 200, func(c *SVG) {
		c.Rotate(45)
		c.Rect(0, 0, 100, 50)
		c.Gend()
	})
	// corners of the 100x50 rect rotated 45 degrees:
	// x in [-50/sqrt2, 100/sqrt2] = [-35.36, 70.71], y in [0, 150/sqrt2] = [0, 106.07]
	wantViewBox(t, out, "-36 0 107 107")
	if c.Err() != nil {
		t.Errorf("unexpected error: %v", c.Err())
	}
}

func TestAutoViewBoxCircleNegative(t *testing.T) {
	out, _ := autoDoc(0, 100, 100, func(c *SVG) {
		c.Circle(-50, -50, 10)
	})
	wantViewBox(t, out, "-60 -60 20 20")
}

func TestAutoViewBoxMargin(t *testing.T) {
	out, _ := autoDoc(10, 100, 100, func(c *SVG) {
		c.Circle(50, 50, 20)
	})
	wantViewBox(t, out, "20 20 60 60")
}

func TestAutoViewBoxText(t *testing.T) {
	out, _ := autoDoc(0, 200, 200, func(c *SVG) {
		c.Text(100, 100, "Hello", "font-size:20px;text-anchor:middle")
	})
	// estimated width 0.6*20*5 = 60, centered: x in [70,130];
	// ascent 16, descent 4: y in [84,104]
	wantViewBox(t, out, "70 84 60 20")
}

func TestAutoViewBoxTextDefaultFontSize(t *testing.T) {
	out, _ := autoDoc(0, 200, 200, func(c *SVG) {
		c.Text(0, 0, "AB") // no font-size: default 16px
	})
	// width 0.6*16*2 = 19.2, ascent 12.8, descent 3.2
	wantViewBox(t, out, "0 -13 20 17")
}

func TestAutoViewBoxMeasureTextHook(t *testing.T) {
	var buf bytes.Buffer
	c := NewAutoViewBox(&buf, 0)
	c.MeasureText = func(s string, size float64) (float64, float64, float64) {
		return 100, 10, 5
	}
	c.Start(200, 200)
	c.Text(0, 0, "x", "font-size:20px")
	c.End()
	wantViewBox(t, buf.String(), "0 -10 100 15")
}

func TestAutoViewBoxNestedTransforms(t *testing.T) {
	out, _ := autoDoc(0, 300, 300, func(c *SVG) {
		c.Translate(100, 100)
		c.Scale(2)
		c.Rect(0, 0, 10, 10)
		c.Gend()
		c.Gend()
	})
	wantViewBox(t, out, "100 100 20 20")
}

func TestAutoViewBoxScaleRotateNested(t *testing.T) {
	out, _ := autoDoc(0, 300, 300, func(c *SVG) {
		c.Scale(2)
		c.Rotate(90)
		c.Rect(0, 0, 10, 5)
		c.Gend()
		c.Gend()
	})
	// rotate(90) maps (x,y) -> (-y,x): corners (0,0),(0,10),(-5,10),(-5,0);
	// then scale(2): (-10,0)..(0,20)
	wantViewBox(t, out, "-10 0 10 20")
}

func TestAutoViewBoxStrokeWidth(t *testing.T) {
	out, _ := autoDoc(0, 100, 100, func(c *SVG) {
		c.Rect(0, 0, 10, 10, "fill:none;stroke:black;stroke-width:4")
	})
	wantViewBox(t, out, "-2 -2 14 14")
}

func TestAutoViewBoxGroupStrokeInherited(t *testing.T) {
	out, _ := autoDoc(0, 100, 100, func(c *SVG) {
		c.Gstyle("stroke:red;stroke-width:10")
		c.Rect(0, 0, 10, 10, "fill:blue")
		c.Gend()
	})
	wantViewBox(t, out, "-5 -5 20 20")
}

func TestAutoViewBoxStrokeUnderTransform(t *testing.T) {
	out, _ := autoDoc(0, 100, 100, func(c *SVG) {
		c.Translate(10, 10)
		c.Rect(0, 0, 10, 10, "stroke-width:4")
		c.Gend()
	})
	wantViewBox(t, out, "8 8 14 14")
}

func TestAutoViewBoxEmpty(t *testing.T) {
	out, c := autoDoc(5, 300, 150, func(c *SVG) {})
	// empty document falls back to the declared dimensions
	wantViewBox(t, out, "0 0 300 150")
	if c.Err() != nil {
		t.Errorf("unexpected error: %v", c.Err())
	}
}

func TestAutoViewBoxZeroAreaLine(t *testing.T) {
	out, _ := autoDoc(0, 100, 100, func(c *SVG) {
		c.Line(5, 5, 5, 5, "stroke:black")
	})
	// degenerate bounds are expanded to a minimal extent
	wantViewBox(t, out, "4 4 2 2")
}

func TestAutoViewBoxNonFiniteTransform(t *testing.T) {
	out, c := autoDoc(0, 100, 100, func(c *SVG) {
		c.Gtransform("scale(1e999)")
		c.Rect(0, 0, 10, 10)
		c.Gend()
	})
	if c.Err() == nil {
		t.Error("expected an error for non-finite transform")
	}
	// the bad transform is treated as identity; the document is still written
	wantViewBox(t, out, "0 0 10 10")
	if !strings.HasSuffix(out, "</svg>\n") {
		t.Error("document not terminated")
	}
}

func TestAutoViewBoxHugeCoordinates(t *testing.T) {
	out, c := autoDoc(0, 100, 100, func(c *SVG) {
		c.Line(0, 0, 1<<40, 1<<40)
	})
	if c.Err() != nil {
		t.Errorf("unexpected error: %v", c.Err())
	}
	wantViewBox(t, out, fmt.Sprintf("0 0 %[1]g %[1]g", float64(int64(1)<<40)))
}

func TestAutoViewBoxPath(t *testing.T) {
	out, _ := autoDoc(0, 200, 200, func(c *SVG) {
		c.Path("M0,0 L100,0 L100,50 Z")
	})
	wantViewBox(t, out, "0 0 100 50")
}

func TestAutoViewBoxBezierExact(t *testing.T) {
	out, _ := autoDoc(0, 200, 200, func(c *SVG) {
		c.Bezier(0, 0, 0, 100, 100, 100, 100, 0)
	})
	// the curve peaks at B(0.5) = 75, not at the control points (100)
	wantViewBox(t, out, "0 0 100 75")
}

func TestAutoViewBoxArc(t *testing.T) {
	out, _ := autoDoc(0, 200, 200, func(c *SVG) {
		c.Arc(0, 0, 50, 50, 0, false, true, 100, 0)
	})
	// semicircle centered at (50,0) r=50, swept through negative y
	wantViewBox(t, out, "0 -50 100 50")
}

func TestAutoViewBoxDefsHidden(t *testing.T) {
	out, _ := autoDoc(0, 100, 100, func(c *SVG) {
		c.Def()
		c.Circle(1000, 1000, 500)
		c.DefEnd()
		c.Circle(10, 10, 5)
	})
	// elements inside defs are not visible and must not contribute
	wantViewBox(t, out, "5 5 10 10")
}

func TestAutoViewBoxDisplayNone(t *testing.T) {
	out, _ := autoDoc(0, 100, 100, func(c *SVG) {
		c.Rect(-1000, -1000, 10, 10, "display:none")
		c.Circle(10, 10, 5)
	})
	wantViewBox(t, out, "5 5 10 10")
}

func TestAutoViewBoxPolygonNegative(t *testing.T) {
	out, _ := autoDoc(0, 100, 100, func(c *SVG) {
		c.Polygon([]int{-10, 10, 0}, []int{-20, -20, 30})
	})
	wantViewBox(t, out, "-10 -20 20 50")
}

func TestAutoViewBoxGridAndTextlines(t *testing.T) {
	out, c := autoDoc(0, 200, 200, func(c *SVG) {
		c.Grid(0, 0, 100, 50, 10, "stroke:gray")
		c.Textlines(10, 20, []string{"ab", "cd"}, 10, 12, "black", "start")
	})
	if c.Err() != nil {
		t.Errorf("unexpected error: %v", c.Err())
	}
	// grid covers [0,100]x[0,50]; text stays inside horizontally,
	// first line ascends to y=20-8=12
	wantViewBox(t, out, "0 0 100 50")
}

func TestAutoViewBoxStable(t *testing.T) {
	draw := func() string {
		out, _ := autoDoc(3, 500, 500, func(c *SVG) {
			c.TranslateRotate(100, 100, 30)
			c.Rect(-20, -10, 40, 20, "stroke-width:2")
			c.Gend()
			c.Circle(-50, 60, 25)
			c.Text(0, 0, "badge", "font-size:14px;text-anchor:middle")
			c.Path("M-10,-10 C 0,-50 50,-30 60,10")
		})
		return out
	}
	if a, b := draw(), draw(); a != b {
		t.Errorf("same input produced different output:\n%s\n---\n%s", a, b)
	}
}

func TestAutoViewBoxBodyMatchesManual(t *testing.T) {
	draw := func(c *SVG) {
		c.Start(200, 200)
		c.Gstyle("fill:none;stroke:black")
		c.Circle(50, 50, 20)
		c.Rect(10, 10, 30, 40)
		c.Gend()
		c.Text(100, 100, "hi", "font-size:12px")
		c.End()
	}
	var manual, auto bytes.Buffer
	draw(New(&manual))
	ac := NewAutoViewBox(&auto, 0)
	draw(ac)
	// the element stream (everything after the header) must be identical
	mbody := manual.String()[strings.Index(manual.String(), "<circle"):]
	abody := auto.String()[strings.Index(auto.String(), "<circle"):]
	if mbody != abody {
		t.Errorf("auto mode changed element output:\nmanual: %q\nauto:   %q", mbody, abody)
	}
}

// manual mode output must be byte-for-byte unchanged

func TestManualStartUnchanged(t *testing.T) {
	var buf bytes.Buffer
	c := New(&buf)
	c.Start(100, 100)
	c.Circle(50, 50, 40, "fill:red")
	c.End()
	want := `<?xml version="1.0"?>
<!-- Generated by SVGo -->
<svg width="100" height="100"
     xmlns="http://www.w3.org/2000/svg"
     xmlns:xlink="http://www.w3.org/1999/xlink">
<circle cx="50" cy="50" r="40" style="fill:red" />
</svg>
`
	if buf.String() != want {
		t.Errorf("manual output changed:\ngot:  %q\nwant: %q", buf.String(), want)
	}
}

func TestManualStartviewUnchanged(t *testing.T) {
	var buf bytes.Buffer
	c := New(&buf)
	c.Startview(100, 100, 0, 0, 50, 50)
	c.Line(0, 0, 50, 50, "stroke:black")
	c.End()
	want := `<?xml version="1.0"?>
<!-- Generated by SVGo -->
<svg width="100" height="100"
     viewBox="0 0 50 50"
     xmlns="http://www.w3.org/2000/svg"
     xmlns:xlink="http://www.w3.org/1999/xlink">
<line x1="0" y1="0" x2="50" y2="50" style="stroke:black" />
</svg>
`
	if buf.String() != want {
		t.Errorf("manual viewBox output changed:\ngot:  %q\nwant: %q", buf.String(), want)
	}
}

func TestAutoViewBoxHeaderShape(t *testing.T) {
	// the auto header matches the manual layout, with viewBox added
	out, _ := autoDoc(0, 100, 100, func(c *SVG) { c.Circle(50, 50, 40) })
	want := `<?xml version="1.0"?>
<!-- Generated by SVGo -->
<svg width="100" height="100" viewBox="10 10 80 80"
     xmlns="http://www.w3.org/2000/svg"
     xmlns:xlink="http://www.w3.org/1999/xlink">
<circle cx="50" cy="50" r="40" />
</svg>
`
	if out != want {
		t.Errorf("auto header mismatch:\ngot:  %q\nwant: %q", out, want)
	}
}

// unit tests for the bounding-box internals

func TestPathBBoxRelative(t *testing.T) {
	b, err := pathBBox("M10,10 l 20,0 l 0,30 z")
	if err != nil {
		t.Fatal(err)
	}
	if b.minx != 10 || b.miny != 10 || b.maxx != 30 || b.maxy != 40 {
		t.Errorf("got %+v", b)
	}
}

func TestPathBBoxSmoothCurves(t *testing.T) {
	// S reflects the previous control point, giving a symmetric downward
	// arch for the second segment: y extent [-75, 75]
	b, err := pathBBox("M0,0 C0,100 100,100 100,0 S200,-100 200,0")
	if err != nil {
		t.Fatal(err)
	}
	if b.minx != 0 || b.maxx != 200 || b.miny != -75 || b.maxy != 75 {
		t.Errorf("got %+v", b)
	}
}

func TestPathBBoxBadData(t *testing.T) {
	if _, err := pathBBox("M1e999,0 L10,10"); err == nil {
		t.Error("expected error for non-finite coordinate")
	}
	if _, err := pathBBox("10 10 20 20"); err == nil {
		t.Error("expected error for missing command")
	}
	if _, err := pathBBox("M0,0 X5,5"); err == nil {
		t.Error("expected error for unknown command")
	}
}

func TestParseTransformList(t *testing.T) {
	m, err := parseTransform("translate(10,20) scale(2) rotate(90)")
	if err != nil {
		t.Fatal(err)
	}
	// p=(1,0): rotate90 -> (0,1); scale2 -> (0,2); translate -> (10,22)
	x, y := m.apply(1, 0)
	if x != 10 || y != 22 {
		t.Errorf("got (%g,%g)", x, y)
	}
}

func TestParseTransformErrors(t *testing.T) {
	for _, s := range []string{"scale(1e999)", "frobnicate(1)", "translate(", "scale(a)"} {
		if _, err := parseTransform(s); err == nil {
			t.Errorf("expected error for %q", s)
		}
	}
}

func TestArcBBoxRotated(t *testing.T) {
	// quarter arc of a circle r=10, x-axis-rotation 90 degrees
	b := arcBBox(0, 0, 10, 10, 90, false, true, 10, -10)
	const eps = 1e-9
	if math.Abs(b.minx-0) > eps || math.Abs(b.miny-(-10)) > eps ||
		math.Abs(b.maxx-10) > eps || math.Abs(b.maxy-0) > eps {
		t.Errorf("got %+v", b)
	}
}
