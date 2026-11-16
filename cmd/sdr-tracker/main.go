// Command sdr-tracker is the entry point for the SDR Aircraft Tracker.
package main

import (
	"context"
	"encoding/json"
	"errors"
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
	if err := run(os.Args[1:]); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintf(os.Stderr, "sdr-tracker: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("sdr-tracker", flag.ContinueOnError)
	mode := fs.String("mode", "tcp",
		`Ingestion mode: "tcp" or "replay"`)
	addr := fs.String("addr", "127.0.0.1:30002",
		"dump1090 AVR raw TCP address")
	replayFile := fs.String("replay-file", "testdata/sample_frames.avr",
		"Path to recorded AVR hex frame file for replay mode")
	replayInterval := fs.Duration("replay-interval", 10*time.Millisecond,
		"Delay between replayed frames; 0 runs at maximum throughput")
	headless := fs.Bool("headless", false,
		"Bypass Bubble Tea TUI; print JSON metrics summary on exit")
	ttl := fs.Duration("ttl", 60*time.Second,
		"Aircraft inactivity eviction timeout")
	logFile := fs.String("log-file", "",
		"Structured JSON slog output path")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *ttl <= 0 || *replayInterval < 0 {
		return fmt.Errorf("ttl must be positive, replay-interval nonnegative, and no positional arguments are accepted")
	}
	if *mode != "tcp" && *mode != "replay" {
		return fmt.Errorf("unknown mode %q", *mode)
	}

	// Configure structured slog JSON logging if requested
	var logCloser io.Closer
	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return fmt.Errorf("open log-file %q: %w", *logFile, err)
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
	sourceErr := make(chan error, 1)
	go func() {
		defer close(rawFrameCh)
		err := src.Run(ctx, rawFrameCh, metrics)
		sourceErr <- err
		if err != nil && !errors.Is(err, context.Canceled) {
			cancel()
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

	var tuiErr error
	if *headless {
		// Headless mode: run until frame stream finishes or user interrupt
		select {
		case <-frameDone:
		case <-ctx.Done():
		}
		cancel()
		<-frameDone
		printSummary(metrics)
	} else {
		// Interactive TUI mode: start Bubble Tea program
		prog := tea.NewProgram(tui.NewModel(tr, metrics, *mode, targetAddr), tea.WithAltScreen())

		go func() {
			<-ctx.Done()
			prog.Quit()
		}()

		if _, err := prog.Run(); err != nil {
			tuiErr = fmt.Errorf("TUI: %w", err)
		}
		cancel()
	}

	cancel()
	<-frameDone
	err := <-sourceErr
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	if tuiErr != nil {
		return tuiErr
	}
	slog.Info("sdr-tracker shutdown complete",
		"total_frames", metrics.TotalFrames.Load(),
		"valid_df17", metrics.ValidDF17Frames.Load(),
	)
	fmt.Fprintln(os.Stderr, "sdr-tracker: shutdown complete")
	return nil
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
