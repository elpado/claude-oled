// claude-oled shows Claude Code usage (5h / 7d limits, context, model, cost)
// on the 128x40 OLED of a SteelSeries keyboard via the GameSense SDK.
package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

var version = "dev"

const usage = `claude-oled – Claude-Code-Nutzung auf dem SteelSeries-OLED

  claude-oled statusline [--chain "<befehl>"]
        Als statusLine-Befehl in ~/.claude/settings.json eintragen. Liest das JSON
        von Claude Code, speichert es für den Daemon und gibt eine Statuszeile aus.
        --chain reicht das JSON an deine bisherige Statusline weiter und zeigt deren Ausgabe.
  claude-oled run        Daemon: zeigt die Daten dauerhaft auf dem OLED an.
  claude-oled demo       Zeigt animierte Beispieldaten auf dem OLED (Test ohne Claude Code).
  claude-oled preview [datei.png]
        Zeigt das aktuelle Bild als ASCII im Terminal (und optional als PNG).
  claude-oled showcase [datei.gif]
        Erzeugt ein animiertes GIF, das alle Stimmungen des Maskottchens durchläuft.
  claude-oled remove     Entfernt die App wieder aus SteelSeries GG.
  claude-oled version    Zeigt die Version.
`

func main() {
	log.SetFlags(log.Ltime)
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "statusline":
		err = cmdStatusline(os.Args[2:])
	case "run":
		err = cmdRun(false)
	case "demo":
		err = cmdRun(true)
	case "preview":
		err = cmdPreview(os.Args[2:])
	case "showcase":
		path := "claude-oled-showcase.gif"
		if len(os.Args) > 2 {
			path = os.Args[2]
		}
		err = cmdShowcase(path)
	case "version":
		fmt.Println(version)
		return
	case "remove":
		var g *GameSense
		if g, err = NewGameSense(); err == nil {
			err = g.Remove()
		}
	default:
		fmt.Print(usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		os.Exit(1)
	}
}

// ---- statusline ------------------------------------------------------------

func cmdStatusline(args []string) error {
	chain := os.Getenv("CLAUDE_OLED_CHAIN")
	for i := 0; i < len(args); i++ {
		if args[i] == "--chain" && i+1 < len(args) {
			chain = args[i+1]
			i++
		}
	}
	raw, _ := io.ReadAll(os.Stdin)

	snap, perr := parseStatusline(raw)
	if perr == nil {
		if prev, err := readSnapshot(statePath()); err == nil {
			snap = mergeSnapshot(prev, snap)
		}
		if err := writeSnapshot(statePath(), snap); err != nil {
			fmt.Fprintln(os.Stderr, "claude-oled:", err)
		}
	}

	if chain != "" {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", chain)
		} else {
			cmd = exec.Command("sh", "-c", chain)
		}
		cmd.Stdin = bytes.NewReader(raw)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	if perr != nil {
		fmt.Println("claude-oled: kein gültiges JSON")
		return nil
	}
	fmt.Println(statusText(snap))
	return nil
}

func statusText(s Snapshot) string {
	parts := []string{}
	if s.Model != "" {
		parts = append(parts, s.Model)
	}
	if s.ContextPercent != nil {
		parts = append(parts, fmt.Sprintf("ctx %.0f%%", *s.ContextPercent))
	}
	if s.FiveHour.UsedPercent != nil {
		parts = append(parts, fmt.Sprintf("5h %.0f%%", *s.FiveHour.UsedPercent))
	}
	if s.SevenDay.UsedPercent != nil {
		parts = append(parts, fmt.Sprintf("7d %.0f%%", *s.SevenDay.UsedPercent))
	}
	return strings.Join(parts, " | ")
}

// ---- daemon ----------------------------------------------------------------

var demoStart = time.Now()

func demoSnapshot(now time.Time) *Snapshot { return demoSnapshotAt(demoStart, now) }

// demoSnapshotAt runs through all moods in a 60 s loop:
// 0-48 s usage climbs from 5 % to 100 %, 48-60 s the mascot "sleeps".
func demoSnapshotAt(start, now time.Time) *Snapshot {
	t := math.Mod(now.Sub(start).Seconds(), 60)
	f := func(v float64) *float64 { return &v }
	r5 := now.Add(2*time.Hour + 13*time.Minute)
	r7 := now.Add(3*24*time.Hour + 4*time.Hour)
	pct := math.Min(100, 5+t*2)
	updated := now
	if t > 48 {
		pct = 30
		updated = now.Add(-42 * time.Minute)
	}
	return &Snapshot{
		UpdatedAt:      updated,
		Model:          "Opus 4.7",
		ContextPercent: f(math.Min(100, 12+t*1.5)),
		CostUSD:        f(1.37 + t/20),
		FiveHour:       Window{UsedPercent: f(pct), ResetsAt: &r5},
		SevenDay:       Window{UsedPercent: f(math.Max(20, pct*0.6)), ResetsAt: &r7},
	}
}

func currentSnapshot() *Snapshot {
	s, err := readSnapshot(statePath())
	if err != nil {
		return nil
	}
	return &s
}

func cmdRun(demo bool) error {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	var g *GameSense
	var last []int
	var lastSent time.Time
	var nextConnect, lastRead time.Time
	var snap *Snapshot
	tick := time.NewTicker(FrameInterval)
	defer tick.Stop()

	log.Printf("Status-Datei: %s", statePath())
	for {
		now := time.Now()
		if g == nil && now.After(nextConnect) {
			gs, err := NewGameSense()
			if err == nil {
				err = gs.Register()
			}
			if err != nil {
				log.Printf("GameSense nicht erreichbar: %v (neuer Versuch in 10s)", err)
				nextConnect = now.Add(10 * time.Second)
			} else {
				log.Printf("Verbunden mit SteelSeries GG (%s)", gs.addr)
				g, last = gs, nil
			}
		}

		if demo {
			snap = demoSnapshot(now)
		} else if now.Sub(lastRead) >= time.Second {
			snap, lastRead = currentSnapshot(), now
		}
		if g != nil {
			bmp := Render(snap, now, int(now.UnixMilli()/FrameInterval.Milliseconds()))
			data := bmp.Bytes()
			if !equalInts(data, last) || now.Sub(lastSent) > 10*time.Second {
				if err := g.Send(bmp); err != nil {
					log.Printf("Senden fehlgeschlagen: %v", err)
					g = nil
					nextConnect = now.Add(3 * time.Second)
				} else {
					last, lastSent = data, now
				}
			}
		}

		select {
		case <-sig:
			if g != nil {
				_ = g.Stop()
			}
			log.Print("Beendet.")
			return nil
		case <-tick.C:
		}
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func cmdPreview(args []string) error {
	now := time.Now()
	snap := currentSnapshot()
	if snap == nil {
		fmt.Fprintln(os.Stderr, "(keine Status-Datei gefunden, zeige Beispieldaten)")
		snap = demoSnapshot(now)
	}
	fmt.Print(Render(snap, now, 0).ASCII())
	if len(args) == 0 {
		return nil
	}
	// animated GIF: 8 s of the current state
	var frames []*Bitmap
	for f := 0; f < 64; f++ {
		frames = append(frames, Render(snap, now, f))
	}
	return SaveGIF(args[0], frames, 4)
}

// cmdShowcase writes a GIF that runs through all moods with demo data.
func cmdShowcase(path string) error {
	base := time.Now()
	var frames []*Bitmap
	for i := 0; i < 8*60; i++ {
		now := base.Add(time.Duration(i) * FrameInterval)
		frames = append(frames, Render(demoSnapshotAt(base, now), now, i))
	}
	return SaveGIF(path, frames, 4)
}
