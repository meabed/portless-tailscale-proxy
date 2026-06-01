package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// makeIcon draws a small "hub" mark — an outer ring with a centred dot — as a
// PNG. Used for the menu-bar icon (idle = black template, running = green).
func makeIcon(col color.NRGBA) []byte {
	const s = 44 // 22pt @2x
	const (
		ring  = 16.0
		width = 4.0
		dot   = 5.0
	)
	cx, cy := float64(s)/2, float64(s)/2
	img := image.NewNRGBA(image.Rect(0, 0, s, s))
	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			// 3×3 supersample for anti-aliasing.
			var cov float64
			for sy := 0; sy < 3; sy++ {
				for sx := 0; sx < 3; sx++ {
					px := float64(x) + (float64(sx)+0.5)/3
					py := float64(y) + (float64(sy)+0.5)/3
					d := math.Hypot(px-cx, py-cy)
					if (d <= ring && d >= ring-width) || d <= dot {
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
