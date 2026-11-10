package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// FrameInterval is how often the daemon draws a new animation frame.
const FrameInterval = 125 * time.Millisecond

type mood int

const (
	moodSleep   mood = iota // nothing happened for a while
	moodHappy               // plenty left
	moodOK                  // working, about half used
	moodWorried             // >= 75 % of a limit used
	moodPanic               // >= 95 % of a limit used
)

// ---- drawing helpers --------------------------------------------------------

func (b *Bitmap) hline(x0, x1, y int) { b.rect(x0, y, x1, y, true) }

func (b *Bitmap) pixels(ox, oy int, rows ...string) {
	for y, row := range rows {
		for x, c := range row {
			if c == '#' {
				b.set(ox+x, oy+y, true)
			}
		}
	}
}

// ellipseOutline draws a 1px outline of an ellipse.
func (b *Bitmap) ellipseOutline(cx, cy, rx, ry float64) {
	in := func(x, y int) bool {
		dx, dy := (float64(x)-cx)/rx, (float64(y)-cy)/ry
		return dx*dx+dy*dy <= 1
	}
	for y := int(cy - ry - 1); y <= int(cy+ry+1); y++ {
		for x := int(cx - rx - 1); x <= int(cx+rx+1); x++ {
			if in(x, y) && (!in(x-1, y) || !in(x+1, y) || !in(x, y-1) || !in(x, y+1)) {
				b.set(x, y, true)
			}
		}
	}
}

// textClip draws text but only inside [x0,x1] – used for the scrolling ticker.
func (b *Bitmap) textClip(x, y, x0, x1 int, s string) {
	for _, r := range s {
		if x > x1 {
			return
		}
		if x+glyphW >= x0 {
			g := glyph(r)
			for gy := 0; gy < glyphH; gy++ {
				for gx := 0; gx < glyphW; gx++ {
					px := x + gx
					if g[gy][gx] == '#' && px >= x0 && px <= x1 {
						b.set(px, y+gy, true)
					}
				}
			}
		}
		x += advance
	}
}

// ---- mascot -----------------------------------------------------------------

// drawMascot draws "Bit", a small blob with an antenna, in the 32x40 area on the left.
func (b *Bitmap) drawMascot(m mood, working bool, frame int) {
	dx, dy := 0, 0
	if working && m != moodSleep && frame%4 < 2 {
		dy = -1 // little bounce while Claude Code is busy
	}
	if m == moodPanic {
		dx = []int{-1, 1, 0, 1, -1, 0}[frame%6] // shiver
	}
	if m == moodSleep {
		// slow breathing
		if frame%16 < 8 {
			dy = 1
		}
	}
	cx, cy := 15+dx, 25+dy

	// body + feet
	b.ellipseOutline(float64(cx), float64(cy), 12.5, 9.5)
	lf, rf := 0, 0
	if working && m != moodSleep {
		if frame%4 < 2 {
			lf = -1
		} else {
			rf = -1
		}
	}
	b.rect(cx-7, cy+10+lf, cx-4, cy+11+lf, true)
	b.rect(cx+4, cy+10+rf, cx+7, cy+11+rf, true)

	// antenna with a twinkling tip
	b.rect(cx, cy-13, cx, cy-10, true)
	tipY := cy - 16
	switch {
	case m == moodSleep:
		b.set(cx, tipY+1, true)
	case frame%8 < 4:
		b.pixels(cx-1, tipY, ".#.", "###", ".#.")
	default:
		b.pixels(cx-2, tipY-1, "#...#", ".#.#.", "..#..", ".#.#.", "#...#")
	}

	// eyes
	ex1, ex2, ey := cx-6, cx+3, cy-4
	blink := frame%40 == 0 || frame%40 == 1
	switch {
	case m == moodSleep:
		b.hline(ex1, ex1+3, ey+2)
		b.hline(ex2, ex2+3, ey+2)
	case m == moodPanic:
		b.pixels(ex1, ey, "#...", ".##.", "...#", ".##.", "#...")
		b.pixels(ex2, ey, "...#", ".##.", "#...", ".##.", "...#")
	case blink:
		b.hline(ex1, ex1+3, ey+2)
		b.hline(ex2, ex2+3, ey+2)
	case m == moodHappy:
		b.pixels(ex1, ey+1, ".##.", "#..#", "#..#")
		b.pixels(ex2, ey+1, ".##.", "#..#", "#..#")
	default:
		b.rect(ex1+1, ey, ex1+2, ey+3, true)
		b.rect(ex2+1, ey, ex2+2, ey+3, true)
	}

	// mouth
	mx, my := cx-3, cy+3
	switch m {
	case moodSleep:
		b.pixels(mx+2, my, "##", "##")
	case moodHappy:
		b.pixels(mx-1, my, "#.....#", ".#...#.", "..###..")
	case moodOK:
		b.pixels(mx, my, "#...#", ".###.")
	case moodWorried:
		b.pixels(mx, my+1, ".#.#.", "#.#.#")
	case moodPanic:
		b.pixels(mx+1, my-1, ".#.", "#.#", "#.#", ".#.")
	}

	// extras
	switch m {
	case moodSleep:
		// Zz drifting up
		p := frame % 24
		zy := 12 - p/3
		b.pixels(cx+9, zy, "###", "..#", ".#.", "#..", "###")
		if p > 8 {
			b.pixels(cx+13, zy-5, "##", ".#", "#.", "##")
		}
	case moodWorried, moodPanic:
		// sweat drop sliding down the side
		sy := cy - 15 + (frame/2)%5
		b.pixels(cx+9, sy, ".#.", "###", "###", ".#.")
	case moodHappy:
		if working && frame%12 < 6 {
			b.pixels(cx-13, cy-11, ".#.", "#.#", ".#.")
		}
	}
}

// ---- bars -------------------------------------------------------------------

const (
	panelX = 34 // right panel starts here
	barL   = panelX + 19
	barR   = W - 26
)

// pill draws a rounded progress bar with animated diagonal stripes.
func (b *Bitmap) pill(y int, label string, pct *float64, timeFrac float64, frame int, animate bool) {
	b.text(panelX, y, label)
	// rounded frame, 7px high
	b.hline(barL+2, barR-2, y)
	b.hline(barL+2, barR-2, y+6)
	b.set(barL+1, y+1, true)
	b.set(barR-1, y+1, true)
	b.set(barL+1, y+5, true)
	b.set(barR-1, y+5, true)
	b.rect(barL, y+2, barL, y+4, true)
	b.rect(barR, y+2, barR, y+4, true)

	if pct == nil {
		b.textRight(W-1, y, "--")
		return
	}
	p := math.Max(0, math.Min(100, *pct))
	inner := barR - barL - 3
	fill := int(math.Round(p / 100 * float64(inner)))
	phase := 0
	if animate {
		phase = frame
	}
	for x := 0; x < fill; x++ {
		for yy := 2; yy <= 4; yy++ {
			px := barL + 2 + x
			// diagonal stripes: one gap every 4 pixels, marching to the right
			if ((px-yy-phase)%4+4)%4 != 0 || fill <= 2 {
				b.set(px, y+yy, true)
			}
		}
	}
	if timeFrac >= 0 && timeFrac <= 1 {
		tx := barL + 2 + int(math.Round(timeFrac*float64(inner-1)))
		b.set(tx, y-1, true)
		b.set(tx, y+7, true)
	}
	txt := fmt.Sprintf("%d%%", int(math.Round(*pct)))
	if *pct >= 95 && frame%8 >= 6 {
		return // blink the number when (almost) out
	}
	b.textRight(W-1, y, txt)
}

// ---- scene ------------------------------------------------------------------

func moodFor(p5, p7 *float64, idle time.Duration) mood {
	if idle > 10*time.Minute {
		return moodSleep
	}
	worst := 0.0
	for _, p := range []*float64{p5, p7} {
		if p != nil && *p > worst {
			worst = *p
		}
	}
	switch {
	case worst >= 95:
		return moodPanic
	case worst >= 75:
		return moodWorried
	case worst >= 40:
		return moodOK
	default:
		return moodHappy
	}
}

func tickerText(s *Snapshot, r5, r7 *time.Time, idle time.Duration, now time.Time) string {
	parts := []string{}
	if r5 != nil {
		parts = append(parts, "5H RESET IN "+fmtDur(r5.Sub(now)))
	}
	if r7 != nil {
		parts = append(parts, "WOCHE RESET IN "+fmtDur(r7.Sub(now)))
	}
	if s.Model != "" {
		parts = append(parts, s.Model)
	}
	if s.CostUSD != nil {
		parts = append(parts, fmt.Sprintf("SESSION $%.2f", *s.CostUSD))
	}
	if idle > 10*time.Minute {
		parts = append(parts, "PAUSE SEIT "+fmtDur(idle)+" ZZZ")
	}
	if len(parts) == 0 {
		parts = append(parts, "CLAUDE CODE")
	}
	return strings.Join(parts, "  *  ") + "  *  "
}

// Render draws one animation frame for a snapshot.
func Render(s *Snapshot, now time.Time, frame int) *Bitmap {
	b := &Bitmap{}
	if s == nil {
		b.drawMascot(moodSleep, false, frame)
		b.text(panelX+6, 8, "WARTE AUF")
		b.text(panelX+6, 20, "CLAUDE CODE")
		return b
	}
	p5, r5 := effective(s.FiveHour, now)
	p7, r7 := effective(s.SevenDay, now)
	idle := now.Sub(s.UpdatedAt)
	working := idle < 45*time.Second
	m := moodFor(p5, p7, idle)

	b.drawMascot(m, working, frame)
	b.pill(1, "5H", p5, elapsedFrac(r5, 5*time.Hour, now), frame, working)
	b.pill(11, "7D", p7, elapsedFrac(r7, 7*24*time.Hour, now), frame, working)
	b.pill(21, "CTX", s.ContextPercent, -1, frame, working)

	// scrolling ticker, 2px per frame
	t := tickerText(s, r5, r7, idle, now)
	tw := len([]rune(t)) * advance
	off := (frame * 2) % tw
	b.textClip(panelX-off, 32, panelX, W-1, t+t)
	return b
}
