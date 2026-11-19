// bbox.go implements the optional automatic viewBox mode.
//
// NewAutoViewBox returns a canvas that buffers its output and, when End is
// called, emits a viewBox attribute computed from the visible bounding boxes
// of every drawn element (paths, basic shapes and text), expanded by a
// configurable margin. Nested group transforms (translate, rotate, scale,
// skew, matrix) and stroke widths are merged into the accumulated bounds.
//
// Notes and defined edge-case behavior:
//   - Empty documents fall back to viewBox="0 0 w h" using the Start
//     dimensions (no viewBox is emitted for Startraw documents).
//   - Degenerate (zero-area) bounds are expanded to a minimal 1-unit extent.
//   - Non-finite transforms or coordinates (NaN, +/-Inf) are recorded via
//     Err(); the offending transform is treated as identity and the
//     offending geometry is skipped.
//   - Text bounds are estimates: font-size defaults to 16px when missing,
//     width is approximated as 0.6em per rune. Assign MeasureText to supply
//     real font metrics.
//   - Elements inside defs, clipPath, mask, marker, pattern and filter
//     blocks are not visible and do not contribute to the bounds.
//   - use, textPath and tspan elements without explicit position do not
//     contribute to the bounds.
//   - The computed viewBox is rounded outward to integer units.

package svg

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// startParams records how the document was started, so the header can be
// replayed (with the computed viewBox) when End is called in auto mode.
type startParams struct {
	w, h int
	unit string
	raw  bool
	ns   []string
}

// autoState holds the automatic viewBox bookkeeping.
type autoState struct {
	margin float64
	buf    bytes.Buffer
	sp     *startParams
	bounds bbox
	gstack []groupState
	hidden int // depth of non-rendering containers (defs, clipPath, ...)
	err    error
}

// groupState is the accumulated state of an open group: the composed
// transform and the inherited stroke and font properties.
type groupState struct {
	ctm         xform
	strokeWidth float64
	strokeSet   bool
	strokeNone  bool
	fontSize    float64
	fontSet     bool
	textAnchor  string
}

// NewAutoViewBox is like New, but the document is buffered and End emits a
// viewBox attribute computed from the bounding boxes of all visible
// elements, expanded by margin user units on every side. A non-finite
// margin is treated as 0 and reported via Err.
func NewAutoViewBox(w io.Writer, margin float64) *SVG {
	s := &SVG{Writer: w, auto: &autoState{margin: margin}}
	if math.IsNaN(margin) || math.IsInf(margin, 0) {
		s.auto.margin = 0
		s.setErr(fmt.Errorf("svg: non-finite viewBox margin %v, using 0", margin))
	}
	return s
}

// Err returns the first error encountered while accumulating bounds in
// automatic viewBox mode (nil in manual mode or when no error occurred).
func (svg *SVG) Err() error {
	if svg.auto == nil {
		return nil
	}
	return svg.auto.err
}

// Bounds returns the accumulated bounding box of all visible elements in
// user coordinates, before the margin is applied. ok is false when no
// element contributed (or in manual mode).
func (svg *SVG) Bounds() (minx, miny, maxx, maxy float64, ok bool) {
	if svg.auto == nil || !svg.auto.bounds.valid {
		return 0, 0, 0, 0, false
	}
	b := svg.auto.bounds
	return b.minx, b.miny, b.maxx, b.maxy, true
}

// out selects the output writer: the buffer in auto mode, the underlying
// writer otherwise.
func (svg *SVG) out() io.Writer {
	if svg.auto != nil {
		return &svg.auto.buf
	}
	return svg.Writer
}

func (svg *SVG) setErr(err error) {
	if svg.auto != nil && svg.auto.err == nil {
		svg.auto.err = err
	}
}

// saveStart records the Start* arguments for replay at End.
func (svg *SVG) saveStart(w, h int, unit string, raw bool, ns []string) {
	// an explicitly supplied viewBox is overridden by the computed one
	var filtered []string
	for _, v := range ns {
		if strings.HasPrefix(strings.TrimSpace(v), "viewBox") {
			continue
		}
		filtered = append(filtered, v)
	}
	svg.auto.sp = &startParams{w: w, h: h, unit: unit, raw: raw, ns: filtered}
}

// endAuto writes the buffered document, emitting the computed viewBox.
func (svg *SVG) endAuto() {
	w := svg.Writer
	if sp := svg.auto.sp; sp != nil {
		fmt.Fprint(w, svgtop)
		if !sp.raw {
			fmt.Fprintf(w, ` width="%d%s" height="%d%s"`, sp.w, sp.unit, sp.h, sp.unit)
		}
		if vb, ok := svg.autoViewBox(); ok {
			fmt.Fprintf(w, " %s", vb)
		}
		for _, v := range sp.ns {
			fmt.Fprintf(w, "\n     %s", v)
		}
		fmt.Fprintln(w, svgns)
	}
	w.Write(svg.auto.buf.Bytes())
	fmt.Fprintln(w, "</svg>")
}

// autoViewBox computes the viewBox attribute from the accumulated bounds.
func (svg *SVG) autoViewBox() (string, bool) {
	a := svg.auto
	if !a.bounds.valid {
		// empty document: fall back to the declared dimensions
		if a.sp != nil && !a.sp.raw {
			return fmt.Sprintf(`viewBox="0 0 %d %d"`, a.sp.w, a.sp.h), true
		}
		return "", false
	}
	m := a.margin
	x0 := a.bounds.minx - m
	y0 := a.bounds.miny - m
	x1 := a.bounds.maxx + m
	y1 := a.bounds.maxy + m
	// degenerate (zero-area) bounds get a minimal 1-unit extent
	if x1-x0 < 1 {
		mid := (x0 + x1) / 2
		x0, x1 = mid-0.5, mid+0.5
	}
	if y1-y0 < 1 {
		mid := (y0 + y1) / 2
		y0, y1 = mid-0.5, mid+0.5
	}
	// snap values within a relative epsilon of an integer, so that
	// floating-point noise (e.g. cos(90deg) = 6e-17) is not amplified
	// into a full unit by the outward rounding below
	scale := math.Max(math.Max(math.Abs(x0), math.Abs(x1)), math.Max(math.Abs(y0), math.Abs(y1)))
	eps := math.Max(scale, 1) * 1e-9
	x0, y0 = snapInt(x0, eps), snapInt(y0, eps)
	x1, y1 = snapInt(x1, eps), snapInt(y1, eps)
	x0, y0 = math.Floor(x0), math.Floor(y0)
	x1, y1 = math.Ceil(x1), math.Ceil(y1)
	if !finite(x0) || !finite(y0) || !finite(x1) || !finite(y1) {
		svg.setErr(fmt.Errorf("svg: non-finite bounds, viewBox omitted"))
		return "", false
	}
	return fmt.Sprintf(`viewBox="%g %g %g %g"`, x0, y0, x1-x0, y1-y0), true
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// snapInt rounds v to the nearest integer when it is within eps of it.
func snapInt(v, eps float64) float64 {
	if r := math.Round(v); math.Abs(v-r) < eps {
		return r
	}
	return v
}

// group stack management

func (svg *SVG) curState() groupState {
	if svg.auto != nil {
		if n := len(svg.auto.gstack); n > 0 {
			return svg.auto.gstack[n-1]
		}
	}
	return groupState{ctm: identity}
}

// bpush opens a group, composing the optional transform string with the
// current transform and merging stroke properties from attrs.
func (svg *SVG) bpush(xf string, attrs []string) {
	if svg.auto == nil {
		return
	}
	st := svg.curState()
	if xf != "" {
		m, err := parseTransform(xf)
		switch {
		case err != nil:
			svg.setErr(err)
		default:
			ctm := st.ctm.mul(m)
			if !ctm.finite() {
				svg.setErr(fmt.Errorf("svg: non-finite composed transform %q, ignored", xf))
				break
			}
			st.ctm = ctm
		}
	}
	m := parseAttrs(attrs)
	if w, ok := strokeWidthProp(m); ok {
		st.strokeWidth, st.strokeSet = w, true
	}
	if v, ok := m["stroke"]; ok {
		st.strokeNone = v == "none"
	}
	if v, ok := m["font-size"]; ok {
		if f, ok2 := parseLength(v); ok2 && f > 0 {
			st.fontSize, st.fontSet = f, true
		}
	}
	if v, ok := m["text-anchor"]; ok {
		st.textAnchor = v
	}
	svg.auto.gstack = append(svg.auto.gstack, st)
}

func (svg *SVG) bpop() {
	if svg.auto == nil {
		return
	}
	if n := len(svg.auto.gstack); n > 0 {
		svg.auto.gstack = svg.auto.gstack[:n-1]
	}
}

// enterHidden/leaveHidden track non-rendering containers (defs, clipPath,
// mask, marker, pattern, filter); their contents are not visible.
func (svg *SVG) enterHidden() {
	if svg.auto != nil {
		svg.auto.hidden++
	}
}

func (svg *SVG) leaveHidden() {
	if svg.auto != nil && svg.auto.hidden > 0 {
		svg.auto.hidden--
	}
}

// bounds accumulation

// accBox merges the local-space rectangle (x0,y0)-(x1,y1), expanded by the
// effective stroke width, into the document bounds. attrs are the element's
// style/attribute strings.
func (svg *SVG) accBox(x0, y0, x1, y1 float64, attrs []string) {
	if svg.auto == nil || svg.auto.hidden > 0 {
		return
	}
	m := parseAttrs(attrs)
	if invisibleProps(m) {
		return
	}
	svg.accBoxRaw(x0, y0, x1, y1, effectiveStrokeWidth(m, svg.curState())/2)
}

// accBBox merges a precomputed local-space bounding box.
func (svg *SVG) accBBox(b bbox, attrs []string) {
	if b.valid {
		svg.accBox(b.minx, b.miny, b.maxx, b.maxy, attrs)
	}
}

// accBoxRaw transforms the (stroke-expanded) local box by the current
// transform and merges it into the document bounds.
func (svg *SVG) accBoxRaw(x0, y0, x1, y1, halfStroke float64) {
	a := svg.auto
	x0 -= halfStroke
	y0 -= halfStroke
	x1 += halfStroke
	y1 += halfStroke
	ctm := svg.curState().ctm
	for _, p := range [4][2]float64{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}} {
		px, py := ctm.apply(p[0], p[1])
		if !finite(px) || !finite(py) {
			svg.setErr(fmt.Errorf("svg: non-finite point (%g,%g), skipped", px, py))
			continue
		}
		a.bounds.addPoint(px, py)
	}
}

// accPoints merges the bounding box of the given coordinate arrays.
func (svg *SVG) accPoints(x, y []int, attrs []string) {
	if svg.auto == nil || svg.auto.hidden > 0 || len(x) == 0 || len(x) != len(y) {
		return
	}
	var b bbox
	for i := range x {
		b.addPoint(float64(x[i]), float64(y[i]))
	}
	svg.accBBox(b, attrs)
}

// accPath parses path data and merges its bounding box.
func (svg *SVG) accPath(d string, attrs []string) {
	if svg.auto == nil || svg.auto.hidden > 0 {
		return
	}
	b, err := pathBBox(d)
	if err != nil {
		svg.setErr(err)
	}
	svg.accBBox(b, attrs)
}

// accText merges the estimated bounding box of text placed at x,y.
func (svg *SVG) accText(x, y int, t string, attrs []string) {
	if svg.auto == nil || svg.auto.hidden > 0 {
		return
	}
	m := parseAttrs(attrs)
	if invisibleProps(m) {
		return
	}
	size := 16.0 // default font size when metrics are missing
	if st := svg.curState(); st.fontSet {
		size = st.fontSize
	}
	if v, ok := m["font-size"]; ok {
		if f, ok2 := parseLength(v); ok2 && f > 0 {
			size = f
		}
	}
	w, asc, desc := svg.measureText(t, size)
	anchor := m["text-anchor"]
	if anchor == "" {
		anchor = svg.curState().textAnchor
	}
	x0 := float64(x)
	switch anchor {
	case "middle":
		x0 -= w / 2
	case "end":
		x0 -= w
	}
	svg.accBoxRaw(x0, float64(y)-asc, x0+w, float64(y)+desc, 0)
}

// measureText returns width, ascent and descent for t at the given font
// size, using the MeasureText hook when set, otherwise an estimate.
func (svg *SVG) measureText(t string, size float64) (w, asc, desc float64) {
	if svg.MeasureText != nil {
		if w, asc, desc = svg.MeasureText(t, size); finite(w) && finite(asc) && finite(desc) && w >= 0 {
			return w, asc, desc
		}
		svg.setErr(fmt.Errorf("svg: MeasureText returned invalid metrics, using estimate"))
	}
	n := float64(len([]rune(t)))
	return 0.6 * size * n, 0.8 * size, 0.2 * size
}

// style and attribute parsing

// parseAttrs converts style strings ("k:v;k:v") and attribute strings
// (`k="v"`, including style="...") into a property map.
func parseAttrs(attrs []string) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		if i := strings.Index(a, "="); i > 0 {
			k := strings.TrimSpace(a[:i])
			v := strings.Trim(strings.TrimSpace(a[i+1:]), `"'`)
			if k == "style" {
				mergeStyle(m, v)
			} else {
				m[k] = v
			}
		} else {
			mergeStyle(m, a)
		}
	}
	return m
}

func mergeStyle(m map[string]string, s string) {
	for _, decl := range strings.Split(s, ";") {
		if i := strings.Index(decl, ":"); i > 0 {
			m[strings.TrimSpace(decl[:i])] = strings.TrimSpace(decl[i+1:])
		}
	}
}

// parseLength parses a CSS length with optional unit (px, pt, em, rem).
func parseLength(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && strings.IndexByte("0123456789+-.eE", s[i]) >= 0 {
		i++
	}
	if i == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(s[:i], 64)
	if err != nil || !finite(v) {
		return 0, false
	}
	switch strings.TrimSpace(s[i:]) {
	case "", "px":
	case "pt":
		v *= 96.0 / 72.0
	case "em", "rem":
		v *= 16
	default:
		return 0, false
	}
	return v, true
}

func strokeWidthProp(m map[string]string) (float64, bool) {
	v, ok := m["stroke-width"]
	if !ok {
		return 0, false
	}
	return parseLength(v)
}

func invisibleProps(m map[string]string) bool {
	return m["display"] == "none" || m["visibility"] == "hidden" || m["visibility"] == "collapse"
}

// effectiveStrokeWidth resolves the stroke width for an element, given its
// own properties and the inherited group state.
func effectiveStrokeWidth(el map[string]string, st groupState) float64 {
	if w, ok := strokeWidthProp(el); ok {
		if el["stroke"] == "none" {
			return 0
		}
		return w
	}
	if el["stroke"] == "none" {
		return 0
	}
	if st.strokeSet {
		if st.strokeNone {
			return 0
		}
		return st.strokeWidth
	}
	if _, ok := el["stroke"]; ok {
		return 1 // SVG default stroke-width
	}
	return 0
}

// 2D affine transforms: x' = a*x + c*y + e, y' = b*x + d*y + f

type xform struct{ a, b, c, d, e, f float64 }

var identity = xform{1, 0, 0, 1, 0, 0}

// mul returns m composed with n: (m*n).apply(p) == m.apply(n.apply(p)).
func (m xform) mul(n xform) xform {
	return xform{
		a: m.a*n.a + m.c*n.b,
		b: m.b*n.a + m.d*n.b,
		c: m.a*n.c + m.c*n.d,
		d: m.b*n.c + m.d*n.d,
		e: m.a*n.e + m.c*n.f + m.e,
		f: m.b*n.e + m.d*n.f + m.f,
	}
}

func (m xform) apply(x, y float64) (float64, float64) {
	return m.a*x + m.c*y + m.e, m.b*x + m.d*y + m.f
}

func (m xform) finite() bool {
	return finite(m.a) && finite(m.b) && finite(m.c) && finite(m.d) && finite(m.e) && finite(m.f)
}

// parseTransform parses an SVG transform list into a single matrix.
func parseTransform(s string) (xform, error) {
	m := identity
	i, n := 0, len(s)
	for i < n {
		for i < n && (s[i] == ' ' || s[i] == ',' || s[i] == '\t' || s[i] == '\n') {
			i++
		}
		if i >= n {
			break
		}
		start := i
		for i < n && (s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z') {
			i++
		}
		name := s[start:i]
		for i < n && s[i] == ' ' {
			i++
		}
		if name == "" || i >= n || s[i] != '(' {
			return m, fmt.Errorf("svg: malformed transform %q", s)
		}
		i++
		var args []float64
		for {
			for i < n && (s[i] == ' ' || s[i] == ',') {
				i++
			}
			if i >= n {
				return m, fmt.Errorf("svg: unterminated transform %q", s)
			}
			if s[i] == ')' {
				i++
				break
			}
			start = i
			for i < n && strings.IndexByte("0123456789+-.eE", s[i]) >= 0 {
				i++
			}
			v, err := strconv.ParseFloat(s[start:i], 64)
			if err != nil {
				return m, fmt.Errorf("svg: bad number in transform %q", s)
			}
			if !finite(v) {
				return m, fmt.Errorf("svg: non-finite value in transform %q", s)
			}
			args = append(args, v)
		}
		op, err := transformOp(name, args)
		if err != nil {
			return m, err
		}
		m = m.mul(op)
	}
	return m, nil
}

func transformOp(name string, args []float64) (xform, error) {
	bad := func(want string) (xform, error) {
		return identity, fmt.Errorf("svg: transform %s expects %s, got %d value(s)", name, want, len(args))
	}
	switch name {
	case "matrix":
		if len(args) != 6 {
			return bad("6 values")
		}
		return xform{args[0], args[1], args[2], args[3], args[4], args[5]}, nil
	case "translate":
		if len(args) < 1 || len(args) > 2 {
			return bad("1 or 2 values")
		}
		ty := 0.0
		if len(args) == 2 {
			ty = args[1]
		}
		return xform{1, 0, 0, 1, args[0], ty}, nil
	case "scale":
		if len(args) < 1 || len(args) > 2 {
			return bad("1 or 2 values")
		}
		sy := args[0]
		if len(args) == 2 {
			sy = args[1]
		}
		return xform{args[0], 0, 0, sy, 0, 0}, nil
	case "rotate":
		if len(args) != 1 && len(args) != 3 {
			return bad("1 or 3 values")
		}
		r := args[0] * math.Pi / 180
		cos, sin := math.Cos(r), math.Sin(r)
		m := xform{cos, sin, -sin, cos, 0, 0}
		if len(args) == 3 {
			cx, cy := args[1], args[2]
			m = xform{1, 0, 0, 1, cx, cy}.mul(m).mul(xform{1, 0, 0, 1, -cx, -cy})
		}
		return m, nil
	case "skewX":
		if len(args) != 1 {
			return bad("1 value")
		}
		return xform{1, 0, math.Tan(args[0] * math.Pi / 180), 1, 0, 0}, nil
	case "skewY":
		if len(args) != 1 {
			return bad("1 value")
		}
		return xform{1, math.Tan(args[0] * math.Pi / 180), 0, 1, 0, 0}, nil
	}
	return identity, fmt.Errorf("svg: unsupported transform %q", name)
}

// bounding boxes

type bbox struct {
	minx, miny, maxx, maxy float64
	valid                  bool
}

func (b *bbox) addPoint(x, y float64) {
	if !b.valid {
		b.minx, b.maxx = x, x
		b.miny, b.maxy = y, y
		b.valid = true
		return
	}
	if x < b.minx {
		b.minx = x
	}
	if x > b.maxx {
		b.maxx = x
	}
	if y < b.miny {
		b.miny = y
	}
	if y > b.maxy {
		b.maxy = y
	}
}

func (b *bbox) add(o bbox) {
	if o.valid {
		b.addPoint(o.minx, o.miny)
		b.addPoint(o.maxx, o.maxy)
	}
}

// cubicBBox returns the exact bounding box of a cubic Bezier segment.
func cubicBBox(x0, y0, x1, y1, x2, y2, x3, y3 float64) bbox {
	var b bbox
	b.addPoint(x0, y0)
	b.addPoint(x3, y3)
	for _, t := range cubicExtrema(x0, x1, x2, x3) {
		b.addPoint(cubicAt(x0, x1, x2, x3, t), cubicAt(y0, y1, y2, y3, t))
	}
	for _, t := range cubicExtrema(y0, y1, y2, y3) {
		b.addPoint(cubicAt(x0, x1, x2, x3, t), cubicAt(y0, y1, y2, y3, t))
	}
	return b
}

func cubicAt(p0, p1, p2, p3, t float64) float64 {
	mt := 1 - t
	return mt*mt*mt*p0 + 3*mt*mt*t*p1 + 3*mt*t*t*p2 + t*t*t*p3
}

// cubicExtrema returns the parameters t in (0,1) where the cubic's
// derivative vanishes.
func cubicExtrema(p0, p1, p2, p3 float64) []float64 {
	// B(t) = A t^3 + B t^2 + C t + p0; B'(t) = 3A t^2 + 2B t + C
	A := -p0 + 3*p1 - 3*p2 + p3
	B := 3*p0 - 6*p1 + 3*p2
	C := -3*p0 + 3*p1
	return solveQuad(3*A, 2*B, C)
}

// quadBBox returns the exact bounding box of a quadratic Bezier segment.
func quadBBox(x0, y0, x1, y1, x2, y2 float64) bbox {
	var b bbox
	b.addPoint(x0, y0)
	b.addPoint(x2, y2)
	for _, t := range quadExtrema(x0, x1, x2) {
		b.addPoint(quadAt(x0, x1, x2, t), quadAt(y0, y1, y2, t))
	}
	for _, t := range quadExtrema(y0, y1, y2) {
		b.addPoint(quadAt(x0, x1, x2, t), quadAt(y0, y1, y2, t))
	}
	return b
}

func quadAt(p0, p1, p2, t float64) float64 {
	mt := 1 - t
	return mt*mt*p0 + 2*mt*t*p1 + t*t*p2
}

func quadExtrema(p0, p1, p2 float64) []float64 {
	// B'(t) = 2(p0-2p1+p2) t + 2(p1-p0)
	d := p0 - 2*p1 + p2
	if d == 0 {
		return nil
	}
	if t := (p0 - p1) / d; t > 0 && t < 1 {
		return []float64{t}
	}
	return nil
}

// solveQuad returns the roots of a t^2 + b t + c = 0 that lie in (0,1).
func solveQuad(a, b, c float64) []float64 {
	const eps = 1e-12
	var roots []float64
	in01 := func(t float64) bool { return t > eps && t < 1-eps }
	if math.Abs(a) < eps {
		if math.Abs(b) > eps {
			if t := -c / b; in01(t) {
				roots = append(roots, t)
			}
		}
		return roots
	}
	disc := b*b - 4*a*c
	if disc < 0 {
		return nil
	}
	sq := math.Sqrt(disc)
	for _, t := range [2]float64{(-b - sq) / (2 * a), (-b + sq) / (2 * a)} {
		if in01(t) {
			roots = append(roots, t)
		}
	}
	return roots
}

// arcBBox returns the bounding box of an elliptical arc in endpoint
// parameterization (SVG 1.1 F.6.5).
func arcBBox(x1, y1, rx, ry, rot float64, large, sweep bool, x2, y2 float64) bbox {
	var b bbox
	b.addPoint(x1, y1)
	b.addPoint(x2, y2)
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 || (x1 == x2 && y1 == y2) {
		return b
	}
	phi := rot * math.Pi / 180
	cosPhi, sinPhi := math.Cos(phi), math.Sin(phi)
	dx, dy := (x1-x2)/2, (y1-y2)/2
	x1p := cosPhi*dx + sinPhi*dy
	y1p := -sinPhi*dx + cosPhi*dy
	if lam := x1p*x1p/(rx*rx) + y1p*y1p/(ry*ry); lam > 1 {
		s := math.Sqrt(lam)
		rx *= s
		ry *= s
	}
	num := rx*rx*ry*ry - rx*rx*y1p*y1p - ry*ry*x1p*x1p
	den := rx*rx*y1p*y1p + ry*ry*x1p*x1p
	coef := 0.0
	if den > 0 {
		coef = math.Sqrt(math.Max(0, num/den))
	}
	if large == sweep {
		coef = -coef
	}
	cxp := coef * rx * y1p / ry
	cyp := -coef * ry * x1p / rx
	cx := cosPhi*cxp - sinPhi*cyp + (x1+x2)/2
	cy := sinPhi*cxp + cosPhi*cyp + (y1+y2)/2
	theta1 := math.Atan2((y1p-cyp)/ry, (x1p-cxp)/rx)
	// signed angle from the start vector to the end vector
	ux, uy := (x1p-cxp)/rx, (y1p-cyp)/ry
	vx, vy := (-x1p-cxp)/rx, (-y1p-cyp)/ry
	dtheta := math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
	if !sweep && dtheta > 0 {
		dtheta -= 2 * math.Pi
	}
	if sweep && dtheta < 0 {
		dtheta += 2 * math.Pi
	}
	point := func(theta float64) (float64, float64) {
		ct, st := math.Cos(theta), math.Sin(theta)
		return cx + rx*ct*cosPhi - ry*st*sinPhi, cy + rx*ct*sinPhi + ry*st*cosPhi
	}
	inSweep := func(theta float64) bool {
		t := math.Mod(theta-theta1, 2*math.Pi)
		if t < 0 {
			t += 2 * math.Pi
		}
		if dtheta >= 0 {
			return t <= dtheta+1e-9
		}
		return t >= 2*math.Pi+dtheta-1e-9
	}
	// extrema of the (rotated) ellipse occur where the tangent is
	// vertical or horizontal
	tx := math.Atan2(-ry*sinPhi, rx*cosPhi)
	ty := math.Atan2(ry*cosPhi, rx*sinPhi)
	for _, theta := range []float64{tx, tx + math.Pi, ty, ty + math.Pi} {
		if inSweep(theta) {
			px, py := point(theta)
			b.addPoint(px, py)
		}
	}
	return b
}

// path data parsing

type dscanner struct {
	s string
	i int
}

func (p *dscanner) skip() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', ',', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *dscanner) hasNum() bool {
	p.skip()
	if p.i >= len(p.s) {
		return false
	}
	c := p.s[p.i]
	return c == '-' || c == '+' || c == '.' || (c >= '0' && c <= '9')
}

func (p *dscanner) num() (float64, error) {
	p.skip()
	start := p.i
	if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
		p.i++
	}
	digits := false
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c >= '0' && c <= '9':
			p.i++
			digits = true
		case c == '.':
			p.i++
		case c == 'e' || c == 'E':
			p.i++
			if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
				p.i++
			}
		default:
			goto done
		}
	}
done:
	if !digits {
		return 0, fmt.Errorf("svg: expected number in path data near %q", p.s[start:])
	}
	tok := p.s[start:p.i]
	v, err := strconv.ParseFloat(tok, 64)
	if err != nil {
		return 0, fmt.Errorf("svg: invalid number %q in path data", tok)
	}
	if !finite(v) {
		return 0, fmt.Errorf("svg: non-finite number %q in path data", tok)
	}
	return v, nil
}

func isCmdChar(c byte) bool {
	return strings.IndexByte("MmZzLlHhVvCcSsQqTtAa", c) >= 0
}

// pathBBox computes the bounding box of SVG path data. Curve bounds are
// exact for Beziers; arcs use the endpoint-to-center conversion.
func pathBBox(d string) (bbox, error) {
	var bb bbox
	p := &dscanner{s: d}
	var cmd, prevCmd byte
	var cx, cy, sx, sy, pcx, pcy float64
	errArgs := func() error { return fmt.Errorf("svg: missing arguments for path command %q in %q", cmd, d) }
	for {
		p.skip()
		if p.i >= len(p.s) {
			break
		}
		if isCmdChar(p.s[p.i]) {
			cmd = p.s[p.i]
			p.i++
		} else if cmd == 0 {
			return bb, fmt.Errorf("svg: path data must begin with a command: %q", d)
		}
		rel := cmd >= 'a' && cmd <= 'z'
		switch cmd {
		case 'M', 'm':
			if !p.hasNum() {
				return bb, errArgs()
			}
			x, err := p.num()
			if err != nil {
				return bb, err
			}
			y, err := p.num()
			if err != nil {
				return bb, err
			}
			if rel {
				x += cx
				y += cy
			}
			cx, cy, sx, sy = x, y, x, y
			bb.addPoint(x, y)
			pcx, pcy = cx, cy
			if rel {
				cmd = 'l'
			} else {
				cmd = 'L'
			}
			prevCmd = cmd
		case 'L', 'l':
			if !p.hasNum() {
				return bb, errArgs()
			}
			for p.hasNum() {
				x, err := p.num()
				if err != nil {
					return bb, err
				}
				y, err := p.num()
				if err != nil {
					return bb, err
				}
				if rel {
					x += cx
					y += cy
				}
				cx, cy = x, y
				bb.addPoint(x, y)
			}
			pcx, pcy = cx, cy
			prevCmd = cmd
		case 'H', 'h':
			if !p.hasNum() {
				return bb, errArgs()
			}
			for p.hasNum() {
				x, err := p.num()
				if err != nil {
					return bb, err
				}
				if rel {
					x += cx
				}
				cx = x
				bb.addPoint(cx, cy)
			}
			pcx, pcy = cx, cy
			prevCmd = cmd
		case 'V', 'v':
			if !p.hasNum() {
				return bb, errArgs()
			}
			for p.hasNum() {
				y, err := p.num()
				if err != nil {
					return bb, err
				}
				if rel {
					y += cy
				}
				cy = y
				bb.addPoint(cx, cy)
			}
			pcx, pcy = cx, cy
			prevCmd = cmd
		case 'C', 'c':
			if !p.hasNum() {
				return bb, errArgs()
			}
			for p.hasNum() {
				v, err := p.nums(6)
				if err != nil {
					return bb, err
				}
				if rel {
					for i := 0; i < 6; i += 2 {
						v[i] += cx
						v[i+1] += cy
					}
				}
				bb.add(cubicBBox(cx, cy, v[0], v[1], v[2], v[3], v[4], v[5]))
				pcx, pcy = v[2], v[3]
				cx, cy = v[4], v[5]
			}
			prevCmd = cmd
		case 'S', 's':
			if !p.hasNum() {
				return bb, errArgs()
			}
			for p.hasNum() {
				v, err := p.nums(4)
				if err != nil {
					return bb, err
				}
				if rel {
					for i := 0; i < 4; i += 2 {
						v[i] += cx
						v[i+1] += cy
					}
				}
				x1, y1 := cx, cy
				if prevCmd == 'C' || prevCmd == 'c' || prevCmd == 'S' || prevCmd == 's' {
					x1, y1 = 2*cx-pcx, 2*cy-pcy
				}
				bb.add(cubicBBox(cx, cy, x1, y1, v[0], v[1], v[2], v[3]))
				pcx, pcy = v[0], v[1]
				cx, cy = v[2], v[3]
			}
			prevCmd = cmd
		case 'Q', 'q':
			if !p.hasNum() {
				return bb, errArgs()
			}
			for p.hasNum() {
				v, err := p.nums(4)
				if err != nil {
					return bb, err
				}
				if rel {
					for i := 0; i < 4; i += 2 {
						v[i] += cx
						v[i+1] += cy
					}
				}
				bb.add(quadBBox(cx, cy, v[0], v[1], v[2], v[3]))
				pcx, pcy = v[0], v[1]
				cx, cy = v[2], v[3]
			}
			prevCmd = cmd
		case 'T', 't':
			if !p.hasNum() {
				return bb, errArgs()
			}
			for p.hasNum() {
				x, err := p.num()
				if err != nil {
					return bb, err
				}
				y, err := p.num()
				if err != nil {
					return bb, err
				}
				if rel {
					x += cx
					y += cy
				}
				x1, y1 := cx, cy
				if prevCmd == 'Q' || prevCmd == 'q' || prevCmd == 'T' || prevCmd == 't' {
					x1, y1 = 2*cx-pcx, 2*cy-pcy
				}
				bb.add(quadBBox(cx, cy, x1, y1, x, y))
				pcx, pcy = x1, y1
				cx, cy = x, y
			}
			prevCmd = cmd
		case 'A', 'a':
			if !p.hasNum() {
				return bb, errArgs()
			}
			for p.hasNum() {
				v, err := p.nums(7)
				if err != nil {
					return bb, err
				}
				x, y := v[5], v[6]
				if rel {
					x += cx
					y += cy
				}
				bb.add(arcBBox(cx, cy, v[0], v[1], v[2], v[3] != 0, v[4] != 0, x, y))
				cx, cy = x, y
			}
			pcx, pcy = cx, cy
			prevCmd = cmd
		case 'Z', 'z':
			cx, cy = sx, sy
			pcx, pcy = cx, cy
			prevCmd = cmd
		default:
			return bb, fmt.Errorf("svg: unsupported path command %q in %q", cmd, d)
		}
	}
	return bb, nil
}

// nums reads exactly n consecutive numbers.
func (p *dscanner) nums(n int) ([]float64, error) {
	v := make([]float64, n)
	for i := range v {
		f, err := p.num()
		if err != nil {
			return nil, err
		}
		v[i] = f
	}
	return v, nil
}
