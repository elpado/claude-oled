package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	gameName  = "CLAUDE_CODE_OLED"
	eventName = "USAGE"
)

func corePropsCandidates() []string {
	if p := os.Getenv("GAMESENSE_COREPROPS"); p != "" {
		return []string{p}
	}
	switch runtime.GOOS {
	case "windows":
		pd := os.Getenv("PROGRAMDATA")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return []string{
			filepath.Join(pd, "SteelSeries", "SteelSeries Engine 3", "coreProps.json"),
			filepath.Join(pd, "SteelSeries", "GG", "coreProps.json"),
		}
	case "darwin":
		return []string{
			"/Library/Application Support/SteelSeries Engine 3/coreProps.json",
			"/Library/Application Support/SteelSeries GG/coreProps.json",
		}
	default:
		return nil
	}
}

// discoverAddress returns host:port of the local GameSense server.
func discoverAddress() (string, error) {
	if a := os.Getenv("GAMESENSE_ADDRESS"); a != "" {
		return a, nil
	}
	for _, p := range corePropsCandidates() {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var cp struct {
			Address string `json:"address"`
		}
		if err := json.Unmarshal(b, &cp); err == nil && cp.Address != "" {
			return cp.Address, nil
		}
	}
	return "", errors.New("coreProps.json nicht gefunden – läuft SteelSeries GG? (oder GAMESENSE_ADDRESS setzen)")
}

type GameSense struct {
	addr   string
	client *http.Client
}

func NewGameSense() (*GameSense, error) {
	addr, err := discoverAddress()
	if err != nil {
		return nil, err
	}
	return &GameSense{addr: addr, client: &http.Client{Timeout: 3 * time.Second}}, nil
}

func (g *GameSense) post(path string, body any) error {
	b, _ := json.Marshal(body)
	resp, err := g.client.Post("http://"+g.addr+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s: HTTP %d: %s", path, resp.StatusCode, bytes.TrimSpace(msg))
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

// Register announces the app to SteelSeries GG and binds a full-screen bitmap
// handler for the 128x40 OLED (Apex Pro, Apex 7, ...). Safe to call repeatedly.
func (g *GameSense) Register() error {
	if err := g.post("/game_metadata", map[string]any{
		"game":                         gameName,
		"game_display_name":            "Claude Code Usage",
		"developer":                    "claude-oled",
		"deinitialize_timer_length_ms": 60000,
	}); err != nil {
		return err
	}
	return g.post("/bind_game_event", map[string]any{
		"game":           gameName,
		"event":          eventName,
		"value_optional": true,
		"handlers": []any{map[string]any{
			"device-type": "screened-128x40",
			"zone":        "one",
			"mode":        "screen",
			"datas": []any{map[string]any{
				"has-text":   false,
				"image-data": make([]int, W*H/8),
			}},
		}},
	})
}

func (g *GameSense) Send(b *Bitmap) error {
	return g.post("/game_event", map[string]any{
		"game":  gameName,
		"event": eventName,
		"data": map[string]any{
			"frame": map[string]any{"image-data-128x40": b.Bytes()},
		},
	})
}

func (g *GameSense) Heartbeat() error {
	return g.post("/game_heartbeat", map[string]any{"game": gameName})
}

func (g *GameSense) Stop() error {
	return g.post("/stop_game", map[string]any{"game": gameName})
}

func (g *GameSense) Remove() error {
	return g.post("/remove_game", map[string]any{"game": gameName})
}
