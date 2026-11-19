// The pathmorph command renders a game-map route arrow that smoothly morphs
// its d attribute from a curved route to a straight one using SMIL.
package main

import (
	"log"
	"os"

	svg "github.com/ajstarks/svgo"
)

func main() {
	width, height := 400, 400
	canvas := svg.New(os.Stdout)
	canvas.Start(width, height)
	canvas.Title("Route arrow path morph")

	// Map background.
	canvas.Rect(0, 0, width, height, `fill="#f4f1ea"`)
	canvas.Grid(0, 0, width, height, 40, "stroke:#ddd6c8")

	// Three frames of the same arrow. The topology is identical in every
	// frame: one moveto, two cubic beziers and a close. Pulling the control
	// points onto the chord turns the bend into a straight arrow.
	curved := "M40,340 C120,300 120,220 200,200 C280,180 280,100 340,60 L360,80 L300,120 C260,150 260,200 200,230 C140,260 90,300 60,360 Z"
	middle := "M40,340 C100,310 130,250 200,215 C270,180 300,140 340,70 L360,80 L300,120 C270,145 270,180 215,215 C160,250 100,285 55,350 Z"
	straight := "M40,340 C90,300 140,260 200,230 C260,200 310,140 340,80 L360,80 L300,120 C280,150 250,185 200,220 C150,255 90,300 50,340 Z"

	canvas.Path(curved, `id="route"`, `fill="#2563eb"`, `stroke="#1e3a8a"`,
		`stroke-width="2"`, `fill-opacity="0.85"`)

	// SMIL d animation: 4 seconds, indefinitely looping, holding at each
	// key time. Validation errors are returned before any markup is written.
	if err := canvas.AnimatePath("#route",
		[]string{curved, middle, straight},
		[]float64{0, 0.5, 1},
		4.0, 0,
		`fill="freeze"`); err != nil {
		log.Fatalf("AnimatePath: %v", err)
	}

	canvas.Text(20, 30, "route arrow: curved to straight (SMIL d morph)",
		`font-family="sans-serif"`, `font-size="16"`, `fill="#333"`)
	canvas.End()
}
