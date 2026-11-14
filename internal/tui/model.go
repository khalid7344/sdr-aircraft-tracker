package tui

import (
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khalid7344/sdr-aircraft-tracker/internal/ingest"
	"github.com/khalid7344/sdr-aircraft-tracker/internal/tracker"
)

// SortMode defines the sorting order for active aircraft in the table view.
type SortMode uint8

const (
	SortLastSeen SortMode = iota
	SortAltitude
	SortMessages
)

// tickMsg triggers periodic snapshot polling and screen redraw.
type tickMsg time.Time

// Model implements the Bubble Tea tea.Model interface.
type Model struct {
	tracker   *tracker.Tracker
	metrics   *ingest.Metrics
	sortMode  SortMode
	mode      string
	addr      string
	startTime time.Time
	width     int
	height    int
	aircraft  []tracker.AircraftState
}

// NewModel constructs an interactive TUI model.
func NewModel(tr *tracker.Tracker, m *ingest.Metrics, mode, addr string) Model {
	return Model{
		tracker:   tr,
		metrics:   m,
		sortMode:  SortLastSeen,
		mode:      mode,
		addr:      addr,
		startTime: time.Now(),
		width:     80,
		height:    24,
	}
}

// Init begins the periodic 250ms tick loop.
func (m Model) Init() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update handles incoming terminal events and timer ticks.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "a":
			m.sortMode = SortAltitude
			m.sortAircraft()
		case "m":
			m.sortMode = SortMessages
			m.sortAircraft()
		case "s":
			m.sortMode = SortLastSeen
			m.sortAircraft()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		if m.tracker != nil {
			m.aircraft = m.tracker.Snapshot()
			m.sortAircraft()
		}
		return m, tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	}

	return m, nil
}

// sortAircraft sorts the cached aircraft slice according to sortMode.
func (m *Model) sortAircraft() {
	switch m.sortMode {
	case SortAltitude:
		sort.Slice(m.aircraft, func(i, j int) bool {
			return m.aircraft[i].AltitudeFeet > m.aircraft[j].AltitudeFeet
		})
	case SortMessages:
		sort.Slice(m.aircraft, func(i, j int) bool {
			return m.aircraft[i].MessagesCount > m.aircraft[j].MessagesCount
		})
	case SortLastSeen:
		sort.Slice(m.aircraft, func(i, j int) bool {
			return m.aircraft[i].LastSeen.After(m.aircraft[j].LastSeen)
		})
	}
}
