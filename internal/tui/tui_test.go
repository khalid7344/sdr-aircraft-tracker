package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khalid7344/sdr-aircraft-tracker/internal/ingest"
	"github.com/khalid7344/sdr-aircraft-tracker/internal/tracker"
)

func TestTUIModelUpdateAndSorting(t *testing.T) {
	metrics := &ingest.Metrics{}
	tr := tracker.NewTracker(60*time.Second, metrics)

	now := time.Now()
	// Aircraft 1: High altitude, low msgs
	ac1 := tracker.AircraftState{
		ICAO:          0x111111,
		ICAOHex:       "111111",
		Callsign:      "TEST1",
		AltitudeFeet:  41000,
		AltitudeValid: true,
		MessagesCount: 10,
		LastSeen:      now.Add(-5 * time.Second),
	}
	// Aircraft 2: Low altitude, high msgs
	ac2 := tracker.AircraftState{
		ICAO:          0x222222,
		ICAOHex:       "222222",
		Callsign:      "TEST2",
		AltitudeFeet:  12000,
		AltitudeValid: true,
		MessagesCount: 50,
		LastSeen:      now,
	}

	m := NewModel(tr, metrics, "replay", "testdata/sample_frames.avr")
	m.aircraft = []tracker.AircraftState{ac1, ac2}

	// 1. Test sort by Altitude ('a')
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model := newM.(Model)
	if model.sortMode != SortAltitude {
		t.Errorf("sortMode = %v, want SortAltitude", model.sortMode)
	}
	if model.aircraft[0].ICAO != 0x111111 {
		t.Errorf("first aircraft after altitude sort = 0x%06X, want 0x111111", model.aircraft[0].ICAO)
	}

	// 2. Test sort by Messages ('m')
	newM, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = newM.(Model)
	if model.sortMode != SortMessages {
		t.Errorf("sortMode = %v, want SortMessages", model.sortMode)
	}
	if model.aircraft[0].ICAO != 0x222222 {
		t.Errorf("first aircraft after messages sort = 0x%06X, want 0x222222", model.aircraft[0].ICAO)
	}

	// 3. Test sort by LastSeen ('s')
	newM, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	model = newM.(Model)
	if model.sortMode != SortLastSeen {
		t.Errorf("sortMode = %v, want SortLastSeen", model.sortMode)
	}
	if model.aircraft[0].ICAO != 0x222222 {
		t.Errorf("first aircraft after lastSeen sort = 0x%06X, want 0x222222", model.aircraft[0].ICAO)
	}

	// 4. Test View rendering contains expected substrings
	viewOutput := model.View()
	if !strings.Contains(viewOutput, "SDR AIRCRAFT TRACKER") {
		t.Error("View output missing title")
	}
	if !strings.Contains(viewOutput, "TEST1") || !strings.Contains(viewOutput, "TEST2") {
		t.Error("View output missing aircraft callsigns")
	}
	if !strings.Contains(viewOutput, "41000") || !strings.Contains(viewOutput, "12000") {
		t.Error("View output missing altitudes")
	}

	// 5. Test Quit ('q')
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Error("quit command was nil on 'q' key")
	}
}
