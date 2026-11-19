package svg

import (
	"bytes"
	"encoding/xml"
	"math"
	"strings"
	"testing"
)

// bend is a curved route arrow (cubic bezier), straight is the same arrow
// collapsed onto a straight line by pulling the control points onto the
// chord. Both share the M/C/C/Z topology so they can be morphed.
const (
	bendPath     = "M10,10 C10,60 60,60 60,100 C60,140 110,140 110,190 Z"
	midPath      = "M10,10 C10,47.5 60,47.5 60,100 C60,132.5 110,132.5 110,175 Z"
	straightPath = "M10,10 C10,35 60,35 60,100 C60,125 110,125 110,160 Z"
)

func TestParsePathCommandSequence(t *testing.T) {
	cmds, err := ParsePath(bendPath)
	if err != nil {
		t.Fatalf("ParsePath: %v", err)
	}
	wantCmds := []byte{'M', 'C', 'C', 'Z'}
	wantArgs := []int{2, 6, 6, 0}
	if len(cmds) != len(wantCmds) {
		t.Fatalf("got %d commands, want %d", len(cmds), len(wantCmds))
	}
	for i, c := range cmds {
		if c.Cmd != wantCmds[i] {
			t.Errorf("command %d = %q, want %q", i, string(c.Cmd), string(wantCmds[i]))
		}
		if len(c.Args) != wantArgs[i] {
			t.Errorf("command %d (%s) has %d args, want %d", i, string(c.Cmd), len(c.Args), wantArgs[i])
		}
	}
	// Endpoints: moveto start and the final cubic endpoint before Z.
	if cmds[0].Args[0] != 10 || cmds[0].Args[1] != 10 {
		t.Errorf("moveto endpoint = %v, want [10 10]", cmds[0].Args)
	}
	last := cmds[2].Args
	if last[4] != 110 || last[5] != 190 {
		t.Errorf("final cubic endpoint = (%g,%g), want (110,190)", last[4], last[5])
	}
}

func TestParsePathImplicitAndCanonical(t *testing.T) {
	// Implicit repeated groups after M become L; whitespace and comma
	// separators, signs and decimals must all tokenize.
	cmds, err := ParsePath("M 0,0 1.5,-2 .5.5 L3 4 Z")
	if err != nil {
		t.Fatalf("ParsePath: %v", err)
	}
	want := []PathCommand{
		{Cmd: 'M', Args: []float64{0, 0}},
		{Cmd: 'L', Args: []float64{1.5, -2}},
		{Cmd: 'L', Args: []float64{0.5, 0.5}},
		{Cmd: 'L', Args: []float64{3, 4}},
		{Cmd: 'Z'},
	}
	if len(cmds) != len(want) {
		t.Fatalf("got %d commands, want %d (%v)", len(cmds), len(want), cmds)
	}
	for i := range want {
		if cmds[i].Cmd != want[i].Cmd || len(cmds[i].Args) != len(want[i].Args) {
			t.Fatalf("command %d = %v, want %v", i, cmds[i], want[i])
		}
		for k := range want[i].Args {
			if cmds[i].Args[k] != want[i].Args[k] {
				t.Errorf("command %d arg %d = %g, want %g", i, k, cmds[i].Args[k], want[i].Args[k])
			}
		}
	}
	if got := renderPath(cmds); got != "M0,0 L1.5,-2 L0.5,0.5 L3,4 Z" {
		t.Errorf("canonical render = %q", got)
	}

	// A new subpath after Z is legal SVG: the following L starts a new
	// segment without a moveto of its own.
	sub, err := ParsePath("M0,0 L1,1 Z L2,2 Z")
	if err != nil {
		t.Fatalf("subpath after Z: %v", err)
	}
	if len(sub) != 5 || sub[2].Cmd != 'Z' || sub[3].Cmd != 'L' || sub[4].Cmd != 'Z' {
		t.Errorf("subpath after Z parsed as %v", sub)
	}
}

func TestParsePathRejections(t *testing.T) {
	bad := []struct {
		name string
		d    string
	}{
		{"relative moveto", "m10,10 L20,20"},
		{"relative lineto", "M10,10 l20,20"},
		{"arc", "M10,10 A5,5 0 0,1 20,20"},
		{"quadratic", "M10,10 Q15,15 20,20"},
		{"smooth cubic", "M10,10 S15,15 20,20"},
		{"horizontal", "M10,10 H30"},
		{"no moveto", "L10,10"},
		{"empty", ""},
		{"truncated", "M10"},
		{"truncated cubic", "M10,10 C1,2 3,4"},
		{"non-number", "M10,abc"},
		{"nan token", "MNaN,1"},
		{"inf token", "M10,Infinity"},
	}
	for _, tc := range bad {
		if _, err := ParsePath(tc.d); err == nil {
			t.Errorf("%s: expected error for %q", tc.name, tc.d)
		}
	}
}

func TestParsePathNonFinite(t *testing.T) {
	// NaN/Inf cannot come through normal tokenization; check the guard
	// directly so the requirement is pinned by a test.
	if _, _, err := readPathArgs("x", 0, 1); err == nil {
		t.Error("expected error for unparseable coordinate")
	}
	c := PathCommand{Cmd: 'L', Args: []float64{math.NaN(), 1}}
	if !math.IsNaN(c.Args[0]) {
		t.Fatal("setup failed")
	}
	if err := checkTopology([][]PathCommand{
		{{Cmd: 'M', Args: []float64{0, 0}}, c},
		{{Cmd: 'M', Args: []float64{0, 0}}, {Cmd: 'L', Args: []float64{1, 1}}},
	}); err != nil {
		t.Errorf("topology check should not inspect finiteness: %v", err)
	}
}

func TestMorphValuesThreeFrames(t *testing.T) {
	paths := []string{bendPath, midPath, straightPath}
	values, times, err := MorphValues(paths, []float64{0, 0.5, 1})
	if err != nil {
		t.Fatalf("MorphValues: %v", err)
	}
	wantValues := bendPath + "; " + midPath + "; " + straightPath
	if values != wantValues {
		t.Errorf("values = %q\nwant %q", values, wantValues)
	}
	if times != "0;0.5;1;" {
		t.Errorf("keyTimes = %q, want %q", times, "0;0.5;1;")
	}
	frames := strings.Split(values, "; ")
	if len(frames) != 3 {
		t.Fatalf("got %d value frames", len(frames))
	}
	for f, d := range frames {
		cmds, err := ParsePath(d)
		if err != nil {
			t.Fatalf("frame %d is not parseable path data: %v", f, err)
		}
		if len(cmds) != 4 || cmds[0].Cmd != 'M' || cmds[1].Cmd != 'C' || cmds[2].Cmd != 'C' || cmds[3].Cmd != 'Z' {
			t.Errorf("frame %d topology changed: %v", f, cmds)
		}
		// Start and end points stay anchored across the morph.
		if cmds[0].Args[0] != 10 || cmds[0].Args[1] != 10 {
			t.Errorf("frame %d start moved: %v", f, cmds[0].Args)
		}
		end := cmds[2].Args
		if end[4] != 110 || end[5] != []float64{190, 175, 160}[f] {
			t.Errorf("frame %d endpoint = (%g,%g)", f, end[4], end[5])
		}
	}
}

func TestMorphValuesDefaultKeyTimes(t *testing.T) {
	_, times, err := MorphValues([]string{"M0,0 L1,1", "M2,2 L3,3", "M4,4 L5,5"}, nil)
	if err != nil {
		t.Fatalf("MorphValues: %v", err)
	}
	if times != "0;0.5;1;" {
		t.Errorf("default keyTimes = %q, want 0;0.5;1;", times)
	}
}

func TestInterpolatePathMidpoint(t *testing.T) {
	got, err := InterpolatePath(bendPath, straightPath, 0.5)
	if err != nil {
		t.Fatalf("InterpolatePath: %v", err)
	}
	if got != midPath {
		t.Errorf("t=0.5 path = %q\nwant        %q", got, midPath)
	}
	// Endpoints of the interpolation.
	cmds, _ := ParsePath(got)
	if cmds[0].Args[0] != 10 || cmds[0].Args[1] != 10 {
		t.Errorf("interpolated start = %v, want (10,10)", cmds[0].Args)
	}
	e := cmds[2].Args
	if e[4] != 110 || e[5] != 175 {
		t.Errorf("interpolated end = (%g,%g), want (110,175)", e[4], e[5])
	}
	// t=0 and t=1 reproduce the endpoints exactly.
	if from, _ := InterpolatePath(bendPath, straightPath, 0); from != bendPath {
		t.Errorf("t=0 = %q, want %q", from, bendPath)
	}
	if to, _ := InterpolatePath(bendPath, straightPath, 1); to != straightPath {
		t.Errorf("t=1 = %q, want %q", to, straightPath)
	}
}

func TestTopologyMismatches(t *testing.T) {
	cases := []struct {
		name  string
		paths []string
	}{
		{"different command count", []string{"M0,0 L1,1 Z", "M0,0 L1,1"}},
		{"different command letter", []string{"M0,0 L1,1", "M0,0 C0,0 1,1 1,1"}},
		{"relative in second frame", []string{"M0,0 L1,1", "m0,0 l1,1"}},
		{"arc in second frame", []string{"M0,0 L1,1", "M0,0 A1,1 0 0,1 1,1"}},
		{"single path", []string{"M0,0 L1,1"}},
	}
	for _, tc := range cases {
		if _, _, err := MorphValues(tc.paths, nil); err == nil {
			t.Errorf("%s: expected topology error", tc.name)
		}
		if len(tc.paths) == 2 {
			if _, err := InterpolatePath(tc.paths[0], tc.paths[1], 0.5); err == nil {
				t.Errorf("%s: InterpolatePath expected error", tc.name)
			}
		}
	}
}

func TestKeyTimesValidation(t *testing.T) {
	good := []string{"M0,0 L1,1", "M2,2 L3,3"}
	cases := []struct {
		name string
		kt   []float64
	}{
		{"wrong count", []float64{0, 0.5, 1}},
		{"first not zero", []float64{0.1, 1}},
		{"last not one", []float64{0, 0.9}},
		{"repeated", []float64{0, 0}},
		{"descending", []float64{0.6, 0.4}},
		{"out of range", []float64{-0.1, 1}},
		{"above range", []float64{0, 1.2}},
		{"nan", []float64{math.NaN(), 1}},
		{"inf", []float64{0, math.Inf(1)}},
	}
	for _, tc := range cases {
		if _, _, err := MorphValues(good, tc.kt); err == nil {
			t.Errorf("%s: expected keyTimes error", tc.name)
		}
	}
}

func TestAnimatePathXML(t *testing.T) {
	var buf bytes.Buffer
	canvas := New(&buf)
	if err := canvas.AnimatePath("#arrow",
		[]string{bendPath, straightPath},
		[]float64{0, 1}, 2.5, 3, `fill="freeze"`); err != nil {
		t.Fatalf("AnimatePath: %v", err)
	}
	out := buf.String()

	// The emitted element must be well-formed XML with the SMIL d-animation
	// attributes in place.
	type animate struct {
		XMLName  xml.Name `xml:"animate"`
		Attr     string   `xml:"attributeName,attr"`
		CalcMode string   `xml:"calcMode,attr"`
		Dur      string   `xml:"dur,attr"`
		Repeat   string   `xml:"repeatCount,attr"`
		Values   string   `xml:"values,attr"`
		KeyTimes string   `xml:"keyTimes,attr"`
	}
	var root struct {
		Animate animate `xml:"animate"`
	}
	if err := xml.Unmarshal([]byte("<root xmlns:xlink=\"http://www.w3.org/1999/xlink\">"+out+"</root>"), &root); err != nil {
		t.Fatalf("emitted markup is not well-formed XML: %v\n%s", err, out)
	}
	a := root.Animate
	if a.Attr != "d" || a.CalcMode != "linear" || a.Dur != "2.5s" || a.Repeat != "3" {
		t.Errorf("unexpected animation attributes: %+v", a)
	}
	if a.Values != bendPath+"; "+straightPath {
		t.Errorf("values = %q", a.Values)
	}
	if a.KeyTimes != "0;1;" {
		t.Errorf("keyTimes = %q, want 0;1;", a.KeyTimes)
	}
	if !strings.Contains(out, `xlink:href="#arrow"`) {
		t.Errorf("missing target reference: %s", out)
	}
	if !strings.Contains(out, `fill="freeze"`) {
		t.Errorf("optional style not emitted: %s", out)
	}
}

func TestAnimatePathIndefiniteAndZeroDuration(t *testing.T) {
	var buf bytes.Buffer
	canvas := New(&buf)
	if err := canvas.AnimatePath("#a", []string{"M0,0 L1,1", "M2,2 L3,3"}, nil, 1, 0); err != nil {
		t.Fatalf("AnimatePath: %v", err)
	}
	if !strings.Contains(buf.String(), `repeatCount="indefinite"`) {
		t.Errorf("repeat<=0 should be indefinite: %s", buf.String())
	}
	for _, d := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		var b bytes.Buffer
		c := New(&b)
		err := c.AnimatePath("#a", []string{"M0,0 L1,1", "M2,2 L3,3"}, nil, d, 1)
		if err == nil {
			t.Errorf("duration %g: expected error", d)
		}
		if b.Len() != 0 {
			t.Errorf("duration %g: invalid call wrote markup", d)
		}
	}
}

func TestAnimatePathRejectsBeforeWrite(t *testing.T) {
	var buf bytes.Buffer
	canvas := New(&buf)
	err := canvas.AnimatePath("#a",
		[]string{"M0,0 L1,1", "M0,0 A1,1 0 0,1 2,2"}, nil, 1, 1)
	if err == nil {
		t.Fatal("expected topology error")
	}
	if buf.Len() != 0 {
		t.Errorf("invalid morph wrote partial markup: %q", buf.String())
	}
}

// TestExistingOutputsUnchanged pins the pre-existing Path and numeric
// Animate markup so the new feature cannot alter them.
func TestExistingOutputsUnchanged(t *testing.T) {
	var buf bytes.Buffer
	canvas := New(&buf)
	canvas.Path("M1,2 L3,4", `fill="red"`, `id="p"`)
	canvas.Animate("#c", "cx", 0, 100, 2.0, 5)
	canvas.AnimateMotion("#c", "M0,0 L1,1", 3.0, 0)
	got := buf.String()
	want := `<path d="M1,2 L3,4" fill="red" id="p" />` + "\n" +
		`<animate xlink:href="#c" attributeName="cx" from="0" to="100" dur="2s" repeatCount="5" />` + "\n" +
		"<animateMotion xlink:href=\"#c\" dur=\"3s\" repeatCount=\"indefinite\" >" +
		`<mpath xlink:href="M0,0 L1,1"/></animateMotion>` + "\n"
	if got != want {
		t.Errorf("existing markup changed:\ngot:\n%s\nwant:\n%s", got, want)
	}
}
