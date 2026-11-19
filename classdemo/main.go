// classdemo compares two ways of painting a picture with many repeated marks:
// inline styles on every shape, and reusable style classes.
//
// The picture has 1000 repeated circles, 1000 repeated text labels, and 100
// nested groups whose child rects take their paint from the group. Both
// documents describe exactly the same picture; the classed one writes each
// paint once as a CSS rule.
//
// Run it with a directory for the generated files:
//
//	go run ./classdemo -o /tmp/classdemo
//
// It prints the size of each document, the number of CSS rules, and the
// largest repeated inline style string, which is what classes eliminate.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ajstarks/svgo"
)

const (
	width, height = 520, 320
	marks         = 1000
	groups        = 100

	markPaint  = "fill:steelblue;fill-opacity:0.8;stroke:navy;stroke-width:1.5"
	labelPaint = "fill:white;font-size:9px;text-anchor:middle"
)

func main() {
	dir := flag.String("o", ".", "directory for inline.svg and classed.svg")
	flag.Parse()

	inline := inlineDocument()
	classed := classedDocument()

	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "classdemo:", err)
		os.Exit(1)
	}
	write(filepath.Join(*dir, "inline.svg"), inline)
	write(filepath.Join(*dir, "classed.svg"), classed)

	inlineText, classedText := string(inline), string(classed)
	fmt.Printf("marks: %d circles, %d labels, %d groups\n", marks, marks, groups)
	fmt.Printf("inline document: %6d bytes, %d style attributes\n",
		len(inline), strings.Count(inlineText, "style="))
	fmt.Printf("classed document: %6d bytes, %d stylesheet, %d CSS rules\n",
		len(classed), strings.Count(classedText, "<style"), strings.Count(classedText, "{"))
	fmt.Printf("saving: %.1fx smaller\n", float64(len(inline))/float64(len(classed)))
}

func inlineDocument() []byte {
	var buf bytes.Buffer
	c := svg.New(&buf)
	c.Start(width, height)
	for i := 0; i < marks; i++ {
		x, y := markPos(i)
		c.Circle(x, y, 4, markPaint)
		c.Text(x, y+2, "x", labelPaint)
	}
	for i := 0; i < groups; i++ {
		c.Gstyle(markPaint)
		c.Gtransform(fmt.Sprintf("translate(%d,%d)", 10+(i%20)*25, 290+(i/20)*6))
		c.Rect(0, 0, 18, 4)
		c.Gend()
		c.Gend()
	}
	c.End()
	return buf.Bytes()
}

func classedDocument() []byte {
	var buf bytes.Buffer
	c := svg.New(&buf)
	c.Start(width, height)
	mark, err := c.StyleClass(markPaint)
	if err != nil {
		panic(err)
	}
	label, err := c.StyleClass(labelPaint)
	if err != nil {
		panic(err)
	}
	for i := 0; i < marks; i++ {
		x, y := markPos(i)
		c.Circle(x, y, 4, svg.ClassAttr(mark))
		c.Text(x, y+2, "x", svg.ClassAttr(label))
	}
	// the group carries the class, so the child rects need no paint of their
	// own; this is how nested groups share a style.
	for i := 0; i < groups; i++ {
		c.Group(svg.ClassAttr(mark))
		c.Gtransform(fmt.Sprintf("translate(%d,%d)", 10+(i%20)*25, 290+(i/20)*6))
		c.Rect(0, 0, 18, 4)
		c.Gend()
		c.Gend()
	}
	c.End()
	return buf.Bytes()
}

func markPos(i int) (x, y int) {
	return 10 + (i%50)*10, 10 + (i/50)*13
}

func write(path string, data []byte) {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "classdemo:", err)
		os.Exit(1)
	}
}
