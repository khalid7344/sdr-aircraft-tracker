package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#5A56E0")).
			Padding(0, 1)

	headerLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#888888"))

	headerValueStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#00D7D7")).
				Bold(true)

	statusConnected = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF87")).
			Bold(true)

	statusDisconnected = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FF5F87")).
				Bold(true)

	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#E0E0E0")).
				Background(lipgloss.Color("#303030")).
				Padding(0, 1)

	rowStyle = lipgloss.NewStyle().
			Padding(0, 1)

	rowAltStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1C1C1C")).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			Italic(true)
)

// View renders the terminal user interface dashboard.
func (m Model) View() string {
	var sb strings.Builder

	// Header: Title and Status
	uptime := time.Since(m.startTime).Round(time.Second)
	connStr := statusDisconnected.Render("DISCONNECTED")
	if m.metrics != nil && m.metrics.Connected.Load() {
		connStr = statusConnected.Render("CONNECTED")
	} else if m.mode == "replay" {
		connStr = statusConnected.Render("REPLAYING")
	}

	titleLine := fmt.Sprintf("%s  Mode: %s (%s)  Status: %s  Uptime: %s",
		titleStyle.Render("SDR AIRCRAFT TRACKER"),
		headerValueStyle.Render(strings.ToUpper(m.mode)),
		m.addr,
		connStr,
		uptime,
	)
	sb.WriteString(titleLine + "\n")

	// Pipeline counters
	var total, valid, crcErr, plausRej, dropped, solved uint64
	if m.metrics != nil {
		total = m.metrics.TotalFrames.Load()
		valid = m.metrics.ValidDF17Frames.Load()
		crcErr = m.metrics.CRCFailedFrames.Load()
		plausRej = m.metrics.PlausibilityRejects.Load()
		dropped = m.metrics.DroppedFrames.Load()
		solved = m.metrics.CPRPositionsSolved.Load()
	}

	metricsLine := fmt.Sprintf("Frames: %s | DF17: %s | CRC Err: %s | Positions: %s | Rejects: %s | Dropped: %s",
		headerValueStyle.Render(fmt.Sprintf("%d", total)),
		headerValueStyle.Render(fmt.Sprintf("%d", valid)),
		headerValueStyle.Render(fmt.Sprintf("%d", crcErr)),
		headerValueStyle.Render(fmt.Sprintf("%d", solved)),
		headerValueStyle.Render(fmt.Sprintf("%d", plausRej)),
		headerValueStyle.Render(fmt.Sprintf("%d", dropped)),
	)
	sb.WriteString(headerLabelStyle.Render(metricsLine) + "\n\n")

	// Table Header
	tableHeader := fmt.Sprintf("%-8s %-9s %8s %9s %8s %11s %11s %7s %7s",
		"ICAO", "CALLSIGN", "ALT(ft)", "SPEED(kt)", "TRACK(°)", "LAT", "LON", "MSGS", "AGE(s)")
	sb.WriteString(tableHeaderStyle.Render(tableHeader) + "\n")

	// Table Rows
	if len(m.aircraft) == 0 {
		sb.WriteString(rowStyle.Render("No active aircraft detected. Ingesting telemetry...") + "\n")
	} else {
		now := time.Now()
		for i, ac := range m.aircraft {
			callsign := ac.Callsign
			if callsign == "" {
				callsign = "—"
			}
			altStr := "—"
			if ac.AltitudeValid {
				altStr = fmt.Sprintf("%d", ac.AltitudeFeet)
			}
			speedStr := "—"
			trackStr := "—"
			if ac.VelocityValid {
				speedStr = fmt.Sprintf("%.1f", ac.GroundSpeedKnots)
				trackStr = fmt.Sprintf("%.1f", ac.TrackDegrees)
			}
			latStr := "—"
			lonStr := "—"
			if ac.PositionValid {
				latStr = fmt.Sprintf("%.4f", ac.Latitude)
				lonStr = fmt.Sprintf("%.4f", ac.Longitude)
			}
			ageSec := int(now.Sub(ac.LastSeen).Seconds())
			if ageSec < 0 {
				ageSec = 0
			}

			row := fmt.Sprintf("%-8s %-9s %8s %9s %8s %11s %11s %7d %7d",
				ac.ICAOHex, callsign, altStr, speedStr, trackStr, latStr, lonStr, ac.MessagesCount, ageSec)

			if i%2 == 1 {
				sb.WriteString(rowAltStyle.Render(row) + "\n")
			} else {
				sb.WriteString(rowStyle.Render(row) + "\n")
			}
		}
	}

	// Footer / Keybindings
	sortDesc := "Seen"
	switch m.sortMode {
	case SortAltitude:
		sortDesc = "Altitude"
	case SortMessages:
		sortDesc = "Messages"
	}
	sb.WriteString("\n" + footerStyle.Render(fmt.Sprintf("Sort: %s (Key: [a] Alt, [m] Msgs, [s] Seen) | [q] Quit", sortDesc)) + "\n")

	return sb.String()
}
