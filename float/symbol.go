// Reusable symbols: define an icon's geometry once in a <symbol> element
// (with its own viewBox), then instantiate it any number of times with
// <use> elements positioned and scaled via a transform.
// Standard Reference: http://www.w3.org/TR/SVG11/struct.html#SymbolElement
package svg

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

// SymbolError describes a problem with a symbol definition or reference.
// Op is the operation that detected the problem ("Symbol", "SymbolEnd",
// "UseSymbol" or "Validate"), ID is the symbol involved, and Seq is the
// sequence number of the symbol operation in the document, so the failing
// call can be located in the generating program.
type SymbolError struct {
	Op  string
	ID  string
	Seq int
	Msg string
}

// Error returns a locatable description of the symbol problem.
func (e *SymbolError) Error() string {
	if e.ID == "" {
		return fmt.Sprintf("svg: %s (symbol op #%d): %s", e.Op, e.Seq, e.Msg)
	}
	return fmt.Sprintf("svg: %s (symbol op #%d): symbol %q: %s", e.Op, e.Seq, e.ID, e.Msg)
}

// symbolDef records a defined symbol and its viewBox.
type symbolDef struct {
	minx, miny, w, h float64
	seq              int
}

// symbolRef records a UseSymbol reference: the referenced id, the enclosing
// symbol (empty at document top level) and where the reference was made.
type symbolRef struct {
	id, from string
	seq      int
	x, y     float64
}

// initSymbols lazily initializes the symbol bookkeeping maps.
func (svg *SVG) initSymbols() {
	if svg.symbols == nil {
		svg.symbols = make(map[string]symbolDef)
	}
}

// Symbol begins a reusable symbol definition with the given id and viewBox
// (minx, miny, width, height); end it with SymbolEnd. Symbols are normally
// placed inside a Def/DefEnd block and instantiated with UseSymbol; the
// geometry is written only once, no matter how many instances are made.
// Symbol definitions may be nested, and a definition may reference other
// symbols with UseSymbol.
// An error is returned (and recorded for ValidateSymbols) if the id is
// already defined or the viewBox is invalid.
// Standard Reference: http://www.w3.org/TR/SVG11/struct.html#SymbolElement
func (svg *SVG) Symbol(id string, minx, miny, width, height float64, s ...string) error {
	svg.initSymbols()
	svg.symSeq++
	seq := svg.symSeq
	var err error
	switch {
	case width <= 0 || height <= 0:
		err = &SymbolError{Op: "Symbol", ID: id, Seq: seq,
			Msg: fmt.Sprintf("invalid viewBox: width and height must be positive, got %g %g", width, height)}
	default:
		if _, dup := svg.symbols[id]; dup {
			err = &SymbolError{Op: "Symbol", ID: id, Seq: seq, Msg: "duplicate symbol definition"}
		}
	}
	if err != nil {
		svg.symErrs = append(svg.symErrs, err)
	}
	svg.symbols[id] = symbolDef{minx: minx, miny: miny, w: width, h: height, seq: seq}
	svg.openSym = append(svg.openSym, id)
	d := svg.Decimals
	svg.printf(`<symbol id="%s" viewBox="%.*f %.*f %.*f %.*f" %s`,
		id, d, minx, d, miny, d, width, d, height, endstyle(s, ">\n"))
	return err
}

// SymbolEnd ends a symbol definition begun with Symbol.
// An error is returned (and recorded) if no symbol definition is open.
func (svg *SVG) SymbolEnd() error {
	svg.initSymbols()
	svg.symSeq++
	if len(svg.openSym) == 0 {
		err := &SymbolError{Op: "SymbolEnd", Seq: svg.symSeq, Msg: "no open symbol definition"}
		svg.symErrs = append(svg.symErrs, err)
		return err
	}
	svg.openSym = svg.openSym[:len(svg.openSym)-1]
	svg.println(`</symbol>`)
	return nil
}

// UseSymbol instantiates the symbol id at position (x, y), scaled by scale.
// The instance is emitted as a single <use> element whose transform places
// the symbol's viewBox origin at (x, y) and scales its geometry by scale;
// a scale of 1 omits the scale term for compact output. The id may be given
// with or without a leading "#". References are checked by ValidateSymbols,
// so a symbol may be used before it is defined, and symbols may reference
// each other (but not cyclically).
// An error is returned (and recorded) if scale is not finite.
// Standard Reference: http://www.w3.org/TR/SVG11/struct.html#UseElement
func (svg *SVG) UseSymbol(x, y float64, id string, scale float64, s ...string) error {
	svg.initSymbols()
	svg.symSeq++
	seq := svg.symSeq
	norm := strings.TrimPrefix(id, "#")
	var err error
	if math.IsNaN(scale) || math.IsInf(scale, 0) {
		err = &SymbolError{Op: "UseSymbol", ID: norm, Seq: seq,
			Msg: fmt.Sprintf("non-finite scale %v at (%g,%g)", scale, x, y)}
		svg.symErrs = append(svg.symErrs, err)
	}
	from := ""
	if n := len(svg.openSym); n > 0 {
		from = svg.openSym[n-1]
	}
	svg.symRefs = append(svg.symRefs, symbolRef{id: norm, from: from, seq: seq, x: x, y: y})
	tf := translate(x, y, svg.Decimals)
	if scale != 1 {
		tf += " " + fmt.Sprintf("scale(%g)", scale)
	}
	svg.printf(`<use %s transform="%s" %s`, href("#"+norm), tf, endstyle(s, emptyclose))
	return err
}

// SymbolBounds returns the bounding box (minx, miny, width, height), in user
// coordinates, of the symbol id instantiated at (x, y) with the given scale.
// An error is returned if the symbol is not defined.
func (svg *SVG) SymbolBounds(id string, x, y, scale float64) (minx, miny, w, h float64, err error) {
	svg.initSymbols()
	norm := strings.TrimPrefix(id, "#")
	def, ok := svg.symbols[norm]
	if !ok {
		err = &SymbolError{Op: "SymbolBounds", ID: norm, Seq: svg.symSeq, Msg: "undefined symbol"}
		return
	}
	return x + def.minx*scale,
		y + def.miny*scale,
		def.w * scale,
		def.h * scale, nil
}

// ValidateSymbols checks every symbol definition and reference made so far
// and returns all problems found, combined into a single error (nil if none):
// duplicate ids, invalid viewBoxes, non-finite scales, references to
// undefined symbols, circular references between symbols, and definitions
// left open without a matching SymbolEnd. Because the check is deferred,
// symbols may be referenced before they are defined. It should be called
// after all definitions and uses, typically just before or after End.
func (svg *SVG) ValidateSymbols() error {
	svg.initSymbols()
	errs := make([]error, len(svg.symErrs))
	copy(errs, svg.symErrs)
	seq := svg.symSeq
	for _, id := range svg.openSym {
		seq++
		errs = append(errs, &SymbolError{Op: "Validate", ID: id, Seq: seq,
			Msg: "unclosed symbol definition (missing SymbolEnd)"})
	}
	for _, r := range svg.symRefs {
		if _, ok := svg.symbols[r.id]; !ok {
			errs = append(errs, &SymbolError{Op: "Validate", ID: r.id, Seq: r.seq,
				Msg: fmt.Sprintf("reference to undefined symbol (used at (%g,%g))", r.x, r.y)})
		}
	}
	errs = append(errs, svg.symbolCycles()...)
	return errors.Join(errs...)
}

// symbolCycles detects circular references between defined symbols.
func (svg *SVG) symbolCycles() []error {
	edges := make(map[string][]string)
	for _, r := range svg.symRefs {
		if r.from != "" {
			edges[r.from] = append(edges[r.from], r.id)
		}
	}
	ids := make([]string, 0, len(svg.symbols))
	for id := range svg.symbols {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	const (
		white = iota // unvisited
		gray         // on the current DFS path
		black        // fully explored
	)
	color := make(map[string]int, len(ids))
	var path []string
	var errs []error
	var visit func(id string)
	visit = func(id string) {
		color[id] = gray
		path = append(path, id)
		for _, next := range edges[id] {
			if _, ok := svg.symbols[next]; !ok {
				continue // undefined references are reported separately
			}
			switch color[next] {
			case gray:
				start := 0
				for i, p := range path {
					if p == next {
						start = i
						break
					}
				}
				cycle := append(append([]string{}, path[start:]...), next)
				errs = append(errs, &SymbolError{Op: "Validate", ID: next, Seq: svg.symSeq,
					Msg: "circular symbol reference: " + strings.Join(cycle, " -> ")})
			case white:
				visit(next)
			}
		}
		path = path[:len(path)-1]
		color[id] = black
	}
	for _, id := range ids {
		if color[id] == white {
			visit(id)
		}
	}
	return errs
}
