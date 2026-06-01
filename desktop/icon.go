package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// Routing fan-out glyph: one entry node on the left routed to three service
// nodes on the right — the proxy's whole job. Positions are canvas fractions.
var (
	glyphEntry = [3]float64{0.20, 0.50, 4.2} // x, y, radius(px)
	glyphNodes = [][3]float64{
		{0.80, 0.255, 3.1},
		{0.825, 0.50, 3.1},
		{0.80, 0.745, 3.1},
	}
)

func segDist(px, py, x1, y1, x2, y2 float64) float64 {
	dx, dy := x2-x1, y2-y1
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return math.Hypot(px-x1, py-y1)
	}
	t := math.Max(0, math.Min(1, ((px-x1)*dx+(py-y1)*dy)/l2))
	return math.Hypot(px-(x1+t*dx), py-(y1+t*dy))
}

// makeIcon renders the glyph in col as a 44px PNG (22pt @2x), anti-aliased.
func makeIcon(col color.NRGBA) []byte {
	const s = 44
	const stroke = 1.7 // connecting-line half-width, px
	ex, ey := glyphEntry[0]*s, glyphEntry[1]*s
	img := image.NewNRGBA(image.Rect(0, 0, s, s))
	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			var cov float64
			for sy := 0; sy < 3; sy++ {
				for sx := 0; sx < 3; sx++ {
					px := float64(x) + (float64(sx)+0.5)/3
					py := float64(y) + (float64(sy)+0.5)/3
					hit := math.Hypot(px-ex, py-ey) <= glyphEntry[2]
					for _, n := range glyphNodes {
						nx, ny := n[0]*s, n[1]*s
						if math.Hypot(px-nx, py-ny) <= n[2] || segDist(px, py, ex, ey, nx, ny) <= stroke {
							hit = true
							break
						}
					}
					if hit {
						cov++
					}
				}
			}
			if cov > 0 {
				c := col
				c.A = uint8(float64(col.A) * cov / 9)
				img.SetNRGBA(x, y, c)
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

var (
	iconIdle    = makeIcon(color.NRGBA{R: 0, G: 0, B: 0, A: 255})     // template (system-tinted)
	iconRunning = makeIcon(color.NRGBA{R: 52, G: 199, B: 89, A: 255}) // green
)
