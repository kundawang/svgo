// Reusable style classes: register a style once, reference it by class from
// any number of shapes, text elements and (nested) groups.
//
// A class is emitted as a single rule in a <style> element written by End.
// Rules are written in registration order, and every rule is written exactly
// once, so documents that repeat the same paint over thousands of elements
// keep one copy of each declaration.
//
// Cascade, from strongest to weakest, is the normal SVG/CSS cascade:
//
//	inline style attribute (the "name:value" style arguments)
//	class rule (from StyleClass)
//	presentation attribute (the "name=value" style arguments)
//	inherited style (for example from a containing group)
//
// Element and group arguments can therefore combine ClassAttr with a local
// style, for example:
//
//	cls, _ := canvas.StyleClass("fill:red;stroke:navy")
//	canvas.Circle(10, 10, 5, ClassAttr(cls), "fill:blue") // fill:blue wins
//	canvas.Group(ClassAttr(parent))                       // inherited by children
//
// See StyleClass for the registration rules. Style errors are reported
// through StyleError, which unwraps to ErrEmptyStyle, ErrInvalidStyle,
// ErrInvalidClassName, ErrClassNameConflict or ErrDocumentClosed, so callers
// can classify failures with errors.Is.

package svg

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var (
	// ErrEmptyStyle is returned when no declaration is supplied.
	ErrEmptyStyle = errors.New("svg: empty style")
	// ErrInvalidStyle is returned when a style declaration is malformed or
	// contains characters that cannot be safely written into a CSS rule.
	ErrInvalidStyle = errors.New("svg: invalid style")
	// ErrInvalidClassName is returned for a class name that is not a CSS
	// identifier accepted by this package.
	ErrInvalidClassName = errors.New("svg: invalid class name")
	// ErrClassNameConflict is returned when an explicit class name is already
	// registered for a different style.
	ErrClassNameConflict = errors.New("svg: class name conflict")
	// ErrDocumentClosed is returned when a class is registered after End.
	ErrDocumentClosed = errors.New("svg: document closed")
)

// StyleError describes a failed class registration. It unwraps to one of the
// sentinel errors above, so errors.Is can be used to classify the failure.
type StyleError struct {
	Op   string // method that failed, for example "StyleClass"
	Name string // explicit class name, if one was supplied
	Msg  string // human readable detail
	Err  error  // one of the Err... sentinels
}

// Error describes the failed class registration.
func (e *StyleError) Error() string {
	if e.Name != "" {
		return fmt.Sprintf("svg: %s %q: %s", e.Op, e.Name, e.Msg)
	}
	return fmt.Sprintf("svg: %s: %s", e.Op, e.Msg)
}

// Unwrap returns the sentinel error for errors.Is.
func (e *StyleError) Unwrap() error { return e.Err }

func newStyleError(op, name, msg string, err error) error {
	return &StyleError{Op: op, Name: name, Msg: msg, Err: err}
}

// StyleClass registers the declarations in style and returns a stable class
// name. Registering the same normalized style again returns the same name and
// does not add another rule. The class is written to the document by End.
//
// Declarations are separated by semicolons and written name:value, as in a
// CSS style attribute. Property names are lower cased, surrounding whitespace
// is removed, runs of unquoted whitespace are collapsed, and when a property
// is repeated the last value wins (CSS semantics). Declaration order is
// otherwise preserved, so shorthand/longhand ordering is not changed.
//
// The returned error wraps ErrEmptyStyle, ErrInvalidStyle or
// ErrDocumentClosed. A failed registration never adds a rule.
func (svg *SVG) StyleClass(style string) (string, error) {
	return svg.registerStyle("StyleClass", "", style)
}

// StyleClassAs registers style under name and returns that class name.
// If the normalized style is already registered under another name, the
// existing name is returned instead so that one style still produces one
// rule. If name is already registered for a different style,
// ErrClassNameConflict is returned and nothing changes.
//
// name must be a CSS identifier: it must start with an ASCII letter or
// underscore, followed by letters, digits, underscores or hyphens. An empty
// name is treated like StyleClass. The returned error also wraps
// ErrInvalidClassName or ErrEmptyStyle/ErrInvalidStyle as appropriate.
func (svg *SVG) StyleClassAs(name, style string) (string, error) {
	return svg.registerStyle("StyleClassAs", name, style)
}

func (svg *SVG) registerStyle(op, name, style string) (string, error) {
	if svg.closed {
		return "", newStyleError(op, name, "class registration after End", ErrDocumentClosed)
	}
	canon, err := canonicalStyle(style)
	if err != nil {
		if errors.Is(err, ErrEmptyStyle) {
			return "", newStyleError(op, name, "no declarations", ErrEmptyStyle)
		}
		return "", newStyleError(op, name, err.Error(), ErrInvalidStyle)
	}
	return svg.registerCanonical(op, name, canon)
}

func (svg *SVG) registerCanonical(op, name, canon string) (string, error) {
	if svg.closed {
		return "", newStyleError(op, name, "class registration after End", ErrDocumentClosed)
	}
	explicit := name != ""
	if explicit && !validClassName(name) {
		return "", newStyleError(op, name,
			"name must start with a letter or underscore and contain only letters, digits, '_' or '-'",
			ErrInvalidClassName)
	}
	// Check the explicit name before the style lookup: using an occupied
	// name for another style is a conflict, even if the new style is already
	// known under some other name.
	if explicit {
		if prev, ok := svg.classNames[name]; ok && prev != canon {
			return "", newStyleError(op, name,
				"class name already registered for a different style", ErrClassNameConflict)
		}
	}
	if svg.classes == nil {
		svg.classes = make(map[string]string)
		svg.classNames = make(map[string]string)
	}
	if existing, ok := svg.classes[canon]; ok {
		return existing, nil
	}
	if !explicit {
		name = svg.nextClassName()
	}
	svg.classes[canon] = name
	svg.classNames[name] = canon
	svg.classOrder = append(svg.classOrder, name)
	return name, nil
}

// nextClassName returns the next unused automatic name (c1, c2, ...).
func (svg *SVG) nextClassName() string {
	for {
		svg.classSeq++
		name := "c" + strconv.Itoa(svg.classSeq)
		if _, used := svg.classNames[name]; !used {
			return name
		}
	}
}

// FillClass registers fill:color and returns its class.
// The color value is validated like any other style value; use "none" for no
// fill.
func (svg *SVG) FillClass(color string) (string, error) {
	return svg.propertyClass("FillClass", "fill", color)
}

// StrokeClass registers stroke:color and returns its class.
func (svg *SVG) StrokeClass(color string) (string, error) {
	return svg.propertyClass("StrokeClass", "stroke", color)
}

// StrokeWidthClass registers stroke-width:width and returns its class.
// width must be finite and greater than zero.
func (svg *SVG) StrokeWidthClass(width float64) (string, error) {
	if math.IsNaN(width) || math.IsInf(width, 0) || width <= 0 {
		return "", newStyleError("StrokeWidthClass", "",
			fmt.Sprintf("stroke width must be a finite value greater than zero, got %v", width),
			ErrInvalidStyle)
	}
	return svg.propertyClass("StrokeWidthClass", "stroke-width", formatCSSNumber(width))
}

// OpacityClass registers opacity:value and returns its class.
// value must be finite and between 0 and 1 inclusive.
func (svg *SVG) OpacityClass(opacity float64) (string, error) {
	return svg.opacityClass("OpacityClass", "opacity", opacity)
}

// FillOpacityClass registers fill-opacity:value and returns its class.
func (svg *SVG) FillOpacityClass(opacity float64) (string, error) {
	return svg.opacityClass("FillOpacityClass", "fill-opacity", opacity)
}

// StrokeOpacityClass registers stroke-opacity:value and returns its class.
func (svg *SVG) StrokeOpacityClass(opacity float64) (string, error) {
	return svg.opacityClass("StrokeOpacityClass", "stroke-opacity", opacity)
}

func (svg *SVG) opacityClass(op, property string, opacity float64) (string, error) {
	if math.IsNaN(opacity) || math.IsInf(opacity, 0) || opacity < 0 || opacity > 1 {
		return "", newStyleError(op, "",
			fmt.Sprintf("opacity must be a finite value between 0 and 1, got %v", opacity),
			ErrInvalidStyle)
	}
	return svg.propertyClass(op, property, formatCSSNumber(opacity))
}

// propertyClass validates value as a single CSS declaration value, so
// semicolons and braces cannot smuggle extra declarations into a helper.
func (svg *SVG) propertyClass(op, property, value string) (string, error) {
	if svg.closed {
		return "", newStyleError(op, "", "class registration after End", ErrDocumentClosed)
	}
	norm, err := normalizeCSSValue(strings.TrimSpace(value))
	if err != nil {
		return "", newStyleError(op, "", fmt.Sprintf("%s: %v", property, err), ErrInvalidStyle)
	}
	return svg.registerCanonical(op, "", property+":"+norm)
}

func formatCSSNumber(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// ClassAttr builds a class="..." attribute suitable for the style arguments
// of shapes, text and groups. Each argument may itself contain several
// space-separated class names. Names are XML escaped, so the result is safe
// to pass through endstyle. Classes are listed in the order supplied; when
// class rules set the same property, the rule registered later wins, as in
// CSS.
func ClassAttr(classes ...string) string {
	var names []string
	for _, c := range classes {
		for _, f := range strings.Fields(c) {
			names = append(names, f)
		}
	}
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`class="`)
	for i, name := range names {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(escapeXMLAttr(name))
	}
	b.WriteByte('"')
	return b.String()
}

// ClassCSS returns the generated stylesheet as plain CSS text, one rule per
// line, in registration order. It returns "" when no classes are registered.
// The text is not XML escaped; End applies XML escaping when writing it into
// the <style> element.
func (svg *SVG) ClassCSS() string {
	var b strings.Builder
	for i, name := range svg.classOrder {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(".")
		b.WriteString(name)
		b.WriteString("{")
		b.WriteString(svg.classNames[name])
		b.WriteString("}")
	}
	return b.String()
}

// writeClassStyle writes the stylesheet, if any, exactly once.
func (svg *SVG) writeClassStyle() {
	if svg.cssWritten || len(svg.classOrder) == 0 {
		return
	}
	svg.println(`<style type="text/css">`)
	for _, name := range svg.classOrder {
		svg.println("." + name + "{" + escapeXMLText(svg.classNames[name]) + "}")
	}
	svg.println(`</style>`)
	svg.cssWritten = true
}

// beginDoc resets class state when the SVG is reused after End, so a second
// document starts with a clean stylesheet. Registration before the first
// Start is preserved.
func (svg *SVG) beginDoc() {
	if !svg.closed {
		return
	}
	svg.classes = nil
	svg.classNames = nil
	svg.classOrder = nil
	svg.classSeq = 0
	svg.cssWritten = false
	svg.closed = false
}

// escapeXMLText escapes a string for XML element content.
func escapeXMLText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// escapeXMLAttr escapes a string for a double-quoted XML attribute value.
func escapeXMLAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;",
		`"`, "&#34;", "'", "&#39;").Replace(s)
}

// canonicalStyle parses a style attribute string into a normalized rule body.
// It returns ErrEmptyStyle when the string contains no declarations and a
// descriptive error (which callers wrap in ErrInvalidStyle) otherwise.
func canonicalStyle(raw string) (string, error) {
	decls, err := splitDeclarations(raw)
	if err != nil {
		return "", err
	}
	var order []string
	values := make(map[string]string)
	for _, decl := range decls {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue // tolerate trailing and repeated semicolons
		}
		colon := strings.IndexByte(decl, ':')
		if colon <= 0 {
			return "", fmt.Errorf("declaration %q: want name:value", decl)
		}
		name := strings.ToLower(strings.TrimSpace(decl[:colon]))
		if !validCSSProperty(name) {
			return "", fmt.Errorf("declaration %q: invalid property name %q", decl, name)
		}
		value, err := normalizeCSSValue(strings.TrimSpace(decl[colon+1:]))
		if err != nil {
			return "", fmt.Errorf("declaration %q: %v", decl, err)
		}
		if _, seen := values[name]; !seen {
			order = append(order, name)
		}
		values[name] = value // last declaration wins, as in CSS
	}
	if len(order) == 0 {
		return "", ErrEmptyStyle
	}
	parts := make([]string, len(order))
	for i, name := range order {
		parts[i] = name + ":" + values[name]
	}
	return strings.Join(parts, ";"), nil
}

// splitDeclarations splits on semicolons that are outside quoted strings.
// It reports an unclosed quote rather than guessing where a value ends.
func splitDeclarations(raw string) ([]string, error) {
	var decls []string
	start := 0
	var quote byte
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case ';':
			decls = append(decls, raw[start:i])
			start = i + 1
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unclosed %q quote", quote)
	}
	return append(decls, raw[start:]), nil
}

// normalizeCSSValue trims and collapses unquoted whitespace, rejects
// characters that could escape a rule, and rejects !important and CSS
// comments so that a class rule can never outrank a local style.
func normalizeCSSValue(value string) (string, error) {
	if value == "" {
		return "", errors.New("empty value")
	}
	var b strings.Builder
	var quote byte
	pendingSpace := false
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c < 0x20 || c == 0x7f {
			return "", fmt.Errorf("control character %q is not allowed", c)
		}
		if quote != 0 {
			b.WriteByte(c)
			if c == '\\' {
				if i+1 < len(value) {
					i++
					b.WriteByte(value[i])
				}
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '{', '}', '<', '>':
			return "", fmt.Errorf("character %q is not allowed", c)
		}
		switch c {
		case '"', '\'':
			if pendingSpace {
				b.WriteByte(' ')
				pendingSpace = false
			}
			quote = c
			b.WriteByte(c)
		case ' ':
			pendingSpace = true
		default:
			if pendingSpace {
				b.WriteByte(' ')
				pendingSpace = false
			}
			b.WriteByte(c)
		}
	}
	if quote != 0 {
		return "", fmt.Errorf("unclosed %q quote", quote)
	}
	norm := strings.TrimSpace(b.String())
	if norm == "" {
		return "", errors.New("empty value")
	}
	if strings.Contains(norm, "/*") || strings.Contains(norm, "*/") {
		return "", errors.New("CSS comments are not allowed")
	}
	if strings.Contains(strings.ToLower(norm), "!important") {
		return "", errors.New("!important is not allowed")
	}
	return norm, nil
}

func validCSSProperty(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	if name[0] >= '0' && name[0] <= '9' {
		return false
	}
	if name[0] == '-' {
		// Allow vendor prefixes (-webkit-x), but not "--custom" or "-3".
		return len(name) > 1 && (name[1] >= 'a' && name[1] <= 'z' || name[1] >= 'A' && name[1] <= 'Z')
	}
	return true
}

func validClassName(name string) bool {
	if name == "" {
		return false
	}
	first := name[0]
	if !(first >= 'a' && first <= 'z' || first >= 'A' && first <= 'Z' || first == '_') {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}
