package svg

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Constrained path morphing.
//
// The numeric Animate methods cannot animate the d attribute of a path,
// because path data is a command stream rather than a scalar value. This
// file implements the restricted, well-defined subset of SVG path data that
// can be interpolated safely:
//
//   - absolute commands only: M, L, C, Z
//   - every frame must use the same command topology (same command letters
//     and the same number of parameters in the same order)
//
// Between two topologically identical paths every coordinate is interpolated
// linearly, so the generated SMIL <animate> element on attributeName="d" is
// legal in every frame.

// PathCommand is one parsed absolute SVG path command.
// Cmd is one of M, L, C, Z; Args holds the command parameters
// (2 for M and L, 6 for C, none for Z).
type PathCommand struct {
	Cmd  byte
	Args []float64
}

// String renders the command in canonical path-data form: the command letter
// followed by comma-separated coordinates, with coordinate pairs separated by
// spaces ("M10,20", "C1,2 3,4 5,6", "Z").
func (c PathCommand) String() string {
	switch len(c.Args) {
	case 0:
		return string(c.Cmd)
	case 2:
		return string(c.Cmd) + fnum(c.Args[0]) + "," + fnum(c.Args[1])
	default:
		p := make([]string, len(c.Args)/2)
		for i := range p {
			p[i] = fnum(c.Args[2*i]) + "," + fnum(c.Args[2*i+1])
		}
		return string(c.Cmd) + strings.Join(p, " ")
	}
}

// pathArgCount is the number of parameters each supported command takes.
func pathArgCount(cmd byte) int {
	switch cmd {
	case 'M', 'L':
		return 2
	case 'C':
		return 6
	case 'Z':
		return 0
	}
	return -1
}

// isPathCmd reports whether b is a supported absolute path command.
func isPathCmd(b byte) bool {
	return b == 'M' || b == 'L' || b == 'C' || b == 'Z'
}

// isPathSep reports whether b is path-data whitespace or a command separator.
func isPathSep(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f', ',':
		return true
	}
	return false
}

// ParsePath parses a restricted SVG path data string into its command stream.
// Only the absolute commands M, L, C and Z are accepted. Relative commands,
// arcs, quadratic curves and any other command are rejected, and every
// coordinate must be a finite number. Implicit command repetition (additional
// coordinate groups after a command) and implicit moveto-lineto rules are
// expanded per the SVG 1.1 path grammar.
func ParsePath(d string) ([]PathCommand, error) {
	var cmds []PathCommand
	i, n := 0, len(d)
	last := byte(0) // last explicitly issued command letter

	for i < n {
		for i < n && isPathSep(d[i]) {
			i++
		}
		if i >= n {
			break
		}
		b := d[i]
		if b >= 'a' && b <= 'z' {
			return nil, fmt.Errorf("pathmorph: relative command %q is not supported, use absolute M/L/C/Z", b)
		}
		if b == 'A' || b == 'Q' || b == 'T' || b == 'S' || b == 'H' || b == 'V' {
			return nil, fmt.Errorf("pathmorph: command %q is not supported, only absolute M/L/C/Z", b)
		}
		if isPathCmd(b) {
			i++
			last = b
			if b == 'Z' {
				cmds = append(cmds, PathCommand{Cmd: 'Z'})
				continue
			}
			args, ni, err := readPathArgs(d, i, pathArgCount(b))
			if err != nil {
				return nil, err
			}
			cmds = append(cmds, PathCommand{Cmd: b, Args: args})
			i = ni
			continue
		}
		// No command letter: an implicit repeat of the previous command.
		// Additional coordinate groups after M are L per the SVG grammar.
		if last == 0 {
			return nil, fmt.Errorf("pathmorph: path data does not begin with a moveto command: %q", d)
		}
		cmd := last
		if cmd == 'M' {
			cmd = 'L'
		}
		if cmd == 'Z' {
			return nil, fmt.Errorf("pathmorph: unexpected data after Z command: %q", d[i:])
		}
		args, ni, err := readPathArgs(d, i, pathArgCount(cmd))
		if err != nil {
			return nil, err
		}
		cmds = append(cmds, PathCommand{Cmd: cmd, Args: args})
		i = ni
	}
	if len(cmds) == 0 {
		return nil, fmt.Errorf("pathmorph: empty path data")
	}
	if cmds[0].Cmd != 'M' {
		return nil, fmt.Errorf("pathmorph: path data must begin with an absolute moveto (M)")
	}
	return cmds, nil
}

// readPathArgs reads exactly count finite numbers starting at index i,
// returning the numbers and the index just past them.
func readPathArgs(d string, i, count int) ([]float64, int, error) {
	args := make([]float64, count)
	for k := 0; k < count; k++ {
		for i < len(d) && isPathSep(d[i]) {
			i++
		}
		if i >= len(d) {
			return nil, i, fmt.Errorf("pathmorph: truncated path data, expected %d more parameter(s)", count-k)
		}
		v, ni, err := readPathNumber(d, i)
		if err != nil {
			return nil, ni, err
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, ni, fmt.Errorf("pathmorph: non-finite coordinate in path data: %q", d)
		}
		args[k] = v
		i = ni
	}
	return args, i, nil
}

// readPathNumber reads one SVG path-data number at index i. Per the SVG
// grammar the sign of a following number also acts as a separator, so a run
// of digits ends a number even without a comma or space.
func readPathNumber(d string, i int) (float64, int, error) {
	start := i
	if i < len(d) && (d[i] == '+' || d[i] == '-') {
		i++
	}
	digits := false
	for i < len(d) && d[i] >= '0' && d[i] <= '9' {
		digits = true
		i++
	}
	if i < len(d) && d[i] == '.' {
		i++
		for i < len(d) && d[i] >= '0' && d[i] <= '9' {
			digits = true
			i++
		}
	}
	if !digits {
		return 0, start, fmt.Errorf("pathmorph: invalid number in path data at %q", d[start:])
	}
	if i < len(d) && (d[i] == 'e' || d[i] == 'E') {
		j := i + 1
		if j < len(d) && (d[j] == '+' || d[j] == '-') {
			j++
		}
		expDigits := false
		for j < len(d) && d[j] >= '0' && d[j] <= '9' {
			expDigits = true
			j++
		}
		if expDigits {
			i = j
		}
	}
	v, err := strconv.ParseFloat(d[start:i], 64)
	if err != nil {
		return 0, start, fmt.Errorf("pathmorph: invalid number %q in path data", d[start:i])
	}
	return v, i, nil
}

// checkTopology verifies that every parsed frame has the same number of
// commands, with the same command letter and parameter count at each
// position.
func checkTopology(frames [][]PathCommand) error {
	base := frames[0]
	for f := 1; f < len(frames); f++ {
		if len(frames[f]) != len(base) {
			return fmt.Errorf("pathmorph: frame %d has %d command(s), frame 0 has %d: command topology differs",
				f, len(frames[f]), len(base))
		}
		for k := range base {
			a, b := base[k], frames[f][k]
			if a.Cmd != b.Cmd {
				return fmt.Errorf("pathmorph: command %d is %q in frame 0 but %q in frame %d: topology mismatch",
					k, string(a.Cmd), string(b.Cmd), f)
			}
			if len(a.Args) != len(b.Args) {
				return fmt.Errorf("pathmorph: command %d (%s) takes %d parameter(s) in frame 0 but %d in frame %d",
					k, string(a.Cmd), len(a.Args), len(b.Args), f)
			}
		}
	}
	return nil
}

// normalizeKeyTimes validates a keyTimes list against the frame count.
// An empty list yields evenly spaced times from 0 to 1. The list must carry
// one finite time per frame, begin at 0, end at 1 and be strictly increasing:
// repeated or out-of-order times are rejected.
func normalizeKeyTimes(keyTimes []float64, frames int) ([]float64, error) {
	if len(keyTimes) == 0 {
		kt := make([]float64, frames)
		for i := range kt {
			kt[i] = float64(i) / float64(frames-1)
		}
		return kt, nil
	}
	if len(keyTimes) != frames {
		return nil, fmt.Errorf("pathmorph: %d keyTimes for %d path frames: the counts must match",
			len(keyTimes), frames)
	}
	for i, t := range keyTimes {
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return nil, fmt.Errorf("pathmorph: non-finite keyTime at index %d", i)
		}
		if t < 0 || t > 1 {
			return nil, fmt.Errorf("pathmorph: keyTime %g out of range [0,1]", t)
		}
		if i == 0 && t != 0 {
			return nil, fmt.Errorf("pathmorph: first keyTime must be 0, got %g", t)
		}
		if i == len(keyTimes)-1 && t != 1 {
			return nil, fmt.Errorf("pathmorph: last keyTime must be 1, got %g", t)
		}
		if i > 0 && t <= keyTimes[i-1] {
			return nil, fmt.Errorf("pathmorph: keyTimes must be strictly increasing: %g followed by %g",
				keyTimes[i-1], t)
		}
	}
	return append([]float64(nil), keyTimes...), nil
}

// formatKeyTimes renders keyTimes for the SMIL keyTimes attribute
// (semicolon-separated, including a trailing semicolon per SVG 1.1).
func formatKeyTimes(keyTimes []float64) string {
	p := make([]string, len(keyTimes))
	for i, t := range keyTimes {
		p[i] = strconv.FormatFloat(t, 'g', -1, 64)
	}
	return strings.Join(p, ";") + ";"
}

// renderPath renders a parsed command stream in canonical path-data form.
func renderPath(cmds []PathCommand) string {
	p := make([]string, len(cmds))
	for i, c := range cmds {
		p[i] = c.String()
	}
	return strings.Join(p, " ")
}

// fnum renders one canonical path coordinate.
func fnum(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// MorphValues parses and validates two or more topologically identical SVG
// paths (absolute M/L/C/Z only) and returns the canonical per-frame path data
// plus the keyTimes list for a SMIL <animate> element targeting the d
// attribute. An empty keyTimes list is replaced by evenly spaced times.
func MorphValues(paths []string, keyTimes []float64) (values, times string, err error) {
	if len(paths) < 2 {
		return "", "", fmt.Errorf("pathmorph: need at least two paths to morph, got %d", len(paths))
	}
	frames := make([][]PathCommand, len(paths))
	for i, d := range paths {
		cmds, perr := ParsePath(d)
		if perr != nil {
			return "", "", fmt.Errorf("frame %d: %v", i, perr)
		}
		frames[i] = cmds
	}
	if err := checkTopology(frames); err != nil {
		return "", "", err
	}
	kt, err := normalizeKeyTimes(keyTimes, len(paths))
	if err != nil {
		return "", "", err
	}
	v := make([]string, len(frames))
	for i, cmds := range frames {
		v[i] = renderPath(cmds)
	}
	return strings.Join(v, "; "), formatKeyTimes(kt), nil
}

// InterpolatePath parses two topologically identical SVG paths and returns
// the canonical path data at time t: t == 0 reproduces from, t == 1
// reproduces to, values in between linearly interpolate every coordinate.
// It is the scalar counterpart of MorphValues, useful for computing single
// intermediate frames.
func InterpolatePath(from, to string, t float64) (string, error) {
	if math.IsNaN(t) || math.IsInf(t, 0) {
		return "", fmt.Errorf("pathmorph: non-finite interpolation time %g", t)
	}
	a, err := ParsePath(from)
	if err != nil {
		return "", fmt.Errorf("from path: %v", err)
	}
	b, err := ParsePath(to)
	if err != nil {
		return "", fmt.Errorf("to path: %v", err)
	}
	if err := checkTopology([][]PathCommand{a, b}); err != nil {
		return "", err
	}
	out := make([]PathCommand, len(a))
	for i := range a {
		c := PathCommand{Cmd: a[i].Cmd, Args: make([]float64, len(a[i].Args))}
		for k := range c.Args {
			c.Args[k] = a[i].Args[k] + (b[i].Args[k]-a[i].Args[k])*t
		}
		out[i] = c
	}
	return renderPath(out), nil
}

// AnimatePath emits a SMIL <animate> element that morphs the d attribute of
// the referenced path through two or more topologically identical SVG paths.
//
// link references the animated path (for example "#arrow"), duration is the
// animation length in seconds and repeat the number of repetitions (zero or
// negative means indefinite). Each entry of paths is one frame of path data
// using only absolute M, L, C and Z commands; keyTimes assigns one strictly
// increasing time per frame from 0 to 1, or may be empty for even spacing.
// Optional style attributes in s are emitted as on the other Animate methods.
//
// All validation happens before anything is written, so an invalid request
// leaves the document untouched.
func (svg *SVG) AnimatePath(link string, paths []string, keyTimes []float64, duration, repeat float64, s ...string) error {
	if duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return fmt.Errorf("pathmorph: duration must be a positive finite number of seconds, got %g", duration)
	}
	values, times, err := MorphValues(paths, keyTimes)
	if err != nil {
		return err
	}
	svg.printf(`<animate %s attributeName="d" calcMode="linear" dur="%gs" repeatCount="%s" values="%s" keyTimes="%s" %s`,
		href(link), duration, repeatString(repeat), values, times, endstyle(s, emptyclose))
	return nil
}
