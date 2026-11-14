// Command sdr-tracker is the entry point for the SDR Aircraft Tracker.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/khalid7344/sdr-aircraft-tracker/internal/adsb"
	"github.com/khalid7344/sdr-aircraft-tracker/internal/ingest"
	"github.com/khalid7344/sdr-aircraft-tracker/internal/tracker"
	"github.com/khalid7344/sdr-aircraft-tracker/internal/tui"
)

func main() {
	mode := flag.String("mode", "tcp",
		`Ingestion mode: "tcp" or "replay"`)
	addr := flag.String("addr", "127.0.0.1:30002",
		"dump1090 AVR raw TCP address")
	replayFile := flag.String("replay-file", "testdata/sample_frames.avr",
		"Path to recorded AVR hex frame file for replay mode")
	replayInterval := flag.Duration("replay-interval", 10*time.Millisecond,
		"Delay between replayed frames; 0 runs at maximum throughput")
	headless := flag.Bool("headless", false,
		"Bypass Bubble Tea TUI; print JSON metrics summary on exit")
	ttl := flag.Duration("ttl", 60*time.Second,
		"Aircraft inactivity eviction timeout")
	logFile := flag.String("log-file", "",
		"Structured JSON slog output path")

	flag.Parse()

	// Configure structured slog JSON logging if requested
	var logCloser io.Closer
	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Fatalf("failed to open log-file %q: %v", *logFile, err)
		}
		logCloser = f
		logger := slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo}))
		slog.SetDefault(logger)
		slog.Info("sdr-tracker started", "mode", *mode, "log_file", *logFile)
	} else {
		// Discard slog output if not enabled
		slog.SetDefault(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	}
	if logCloser != nil {
		defer logCloser.Close()
	}

	// Root context: cancelled on SIGINT, SIGTERM, or TUI exit.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	metrics := &ingest.Metrics{}
	rawFrameCh := make(chan ingest.RawFrame, 1024)
	tr := tracker.NewTracker(*ttl, metrics)
	tr.StartReaper(ctx, 5*time.Second)

	var src ingest.Source
	var targetAddr string
	switch *mode {
	case "tcp":
		src = ingest.NewTCPSource(*addr)
		targetAddr = *addr
	case "replay":
		src = ingest.NewReplaySource(*replayFile, *replayInterval)
		targetAddr = *replayFile
	default:
		fmt.Fprintf(os.Stderr, "sdr-tracker: unknown mode %q (must be \"tcp\" or \"replay\")\n", *mode)
		os.Exit(1)
	}

	// Source goroutine: streams frames until exhausted or canceled, then closes rawFrameCh.
	go func() {
		defer close(rawFrameCh)
		if err := src.Run(ctx, rawFrameCh, metrics); err != nil && err != context.Canceled {
			slog.Warn("source error", "error", err)
		}
	}()

	// Frame decode loop: reads raw frames until rawFrameCh is closed or ctx canceled.
	frameDone := make(chan struct{})
	go func() {
		defer close(frameDone)
		for frame := range rawFrameCh {
			msg, status := adsb.ProcessRawHex(frame.Hex, frame.ReceivedAt)
			switch status {
			case adsb.StatusValidDF17:
				metrics.ValidDF17Frames.Add(1)
				tr.Update(msg)
			case adsb.StatusCRCFailed:
				metrics.CRCFailedFrames.Add(1)
			case adsb.StatusNonDF17:
				metrics.NonDF17Frames.Add(1)
			case adsb.StatusInvalidHex:
				metrics.MalformedFrames.Add(1)
			}
		}
	}()

	if *headless {
		// Headless mode: run until frame stream finishes or user interrupt
		select {
		case <-frameDone:
		case <-ctx.Done():
		}
		printSummary(metrics)
	} else {
		// Interactive TUI mode: start Bubble Tea program
		prog := tea.NewProgram(tui.NewModel(tr, metrics, *mode, targetAddr), tea.WithAltScreen())

		// Goroutine to stop TUI if context is cancelled (e.g. SIGINT or replay done)
		go func() {
			select {
			case <-frameDone:
				if *mode == "replay" {
					// In replay mode, leave TUI running so user can inspect, or quit if desired
				}
			case <-ctx.Done():
				prog.Quit()
			}
		}()

		if _, err := prog.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		}
		cancel()
	}

	slog.Info("sdr-tracker shutdown complete",
		"total_frames", metrics.TotalFrames.Load(),
		"valid_df17", metrics.ValidDF17Frames.Load(),
	)
	fmt.Fprintln(os.Stderr, "sdr-tracker: shutdown complete")
}

// printSummary writes a JSON object of pipeline counters to stdout.
func printSummary(m *ingest.Metrics) {
	type summary struct {
		TotalFrames         uint64 `json:"total_frames"`
		MalformedFrames     uint64 `json:"malformed_frames"`
		NonDF17Frames       uint64 `json:"non_df17_frames"`
		CRCFailedFrames     uint64 `json:"crc_failed_frames"`
		ValidDF17Frames     uint64 `json:"valid_df17_frames"`
		CPRPositionsSolved  uint64 `json:"cpr_positions_solved"`
		PlausibilityRejects uint64 `json:"plausibility_rejects"`
		DroppedFrames       uint64 `json:"dropped_frames"`
		ReconnectCount      uint64 `json:"reconnect_count"`
		Connected           bool   `json:"connected"`
	}
	s := summary{
		TotalFrames:         m.TotalFrames.Load(),
		MalformedFrames:     m.MalformedFrames.Load(),
		NonDF17Frames:       m.NonDF17Frames.Load(),
		CRCFailedFrames:     m.CRCFailedFrames.Load(),
		ValidDF17Frames:     m.ValidDF17Frames.Load(),
		CPRPositionsSolved:  m.CPRPositionsSolved.Load(),
		PlausibilityRejects: m.PlausibilityRejects.Load(),
		DroppedFrames:       m.DroppedFrames.Load(),
		ReconnectCount:      m.ReconnectCount.Load(),
		Connected:           m.Connected.Load(),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		log.Printf("printSummary: %v", err)
	}
}
