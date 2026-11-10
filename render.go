package main

import (
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"strings"
	"time"
)

const W, H = 128, 40

type Bitmap [H][W]bool

func (b *Bitmap) set(x, y int, on bool) {
	if x >= 0 && x < W && y >= 0 && y < H {
		b[y][x] = on
	}
}

func (b *Bitmap) rect(x0, y0, x1, y1 int, on bool) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			b.set(x, y, on)
		}
	}
}

func (b *Bitmap) text(x, y int, s string) {
	for _, r := range s {
		g := glyph(r)
		for gy := 0; gy < glyphH; gy++ {
			for gx := 0; gx < glyphW; gx++ {
				if g[gy][gx] == '#' {
					b.set(x+gx, y+gy, true)
				}
			}
		}
		x += advance
	}
}

func (b *Bitmap) textRight(xRight, y int, s string) { b.text(xRight-textWidth(s)+1, y, s) }

// Bytes packs the bitmap row by row, MSB first, as GameSense expects (640 bytes).
func (b *Bitmap) Bytes() []int {
	out := make([]int, W*H/8)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			if b[y][x] {
				i := y*W + x
				out[i/8] |= 0x80 >> (i % 8)
			}
		}
	}
	return out
}

func (b *Bitmap) ASCII() string {
	var sb strings.Builder
	sb.WriteString("+" + strings.Repeat("-", W) + "+\n")
	for y := 0; y < H; y++ {
		sb.WriteByte('|')
		for x := 0; x < W; x++ {
			if b[y][x] {
				sb.WriteRune('█')
			} else {
				sb.WriteByte(' ')
			}
		}
		sb.WriteString("|\n")
	}
	sb.WriteString("+" + strings.Repeat("-", W) + "+\n")
	return sb.String()
}

func (b *Bitmap) SavePNG(path string, scale int) error {
	img := image.NewGray(image.Rect(0, 0, W*scale, H*scale))
	for y := 0; y < H*scale; y++ {
		for x := 0; x < W*scale; x++ {
			c := color.Gray{Y: 0}
			if b[y/scale][x/scale] {
				c = color.Gray{Y: 255}
			}
			img.SetGray(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func fmtDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	m := int(d.Round(time.Minute).Minutes())
	switch {
	case m < 60:
		return fmt.Sprintf("%dM", m)
	case m < 24*60:
		return fmt.Sprintf("%dH%02d", m/60, m%60)
	default:
		return fmt.Sprintf("%dD%02dH", m/(24*60), (m%(24*60))/60)
	}
}

// effective returns the window as it should be shown now: once its reset time
// has passed, the usage is back to 0.
func effective(w Window, now time.Time) (*float64, *time.Time) {
	if w.UsedPercent != nil && w.ResetsAt != nil && now.After(*w.ResetsAt) {
		zero := 0.0
		return &zero, nil
	}
	return w.UsedPercent, w.ResetsAt
}

func elapsedFrac(resets *time.Time, length time.Duration, now time.Time) float64 {
	if resets == nil {
		return -1
	}
	start := resets.Add(-length)
	return float64(now.Sub(start)) / float64(length)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func (b *Bitmap) paletted(scale int) *image.Paletted {
	pal := color.Palette{color.Black, color.RGBA{255, 255, 255, 255}}
	img := image.NewPaletted(image.Rect(0, 0, W*scale, H*scale), pal)
	for y := 0; y < H*scale; y++ {
		for x := 0; x < W*scale; x++ {
			if b[y/scale][x/scale] {
				img.SetColorIndex(x, y, 1)
			}
		}
	}
	return img
}

// SaveGIF writes the frames as an endlessly looping animated GIF.
func SaveGIF(path string, frames []*Bitmap, scale int) error {
	g := &gif.GIF{}
	delay := int(FrameInterval / (10 * time.Millisecond))
	for _, f := range frames {
		g.Image = append(g.Image, f.paletted(scale))
		g.Delay = append(g.Delay, delay)
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	return gif.EncodeAll(out, g)
}
