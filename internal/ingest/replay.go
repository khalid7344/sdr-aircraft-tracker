package ingest

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"time"
)

// ReplaySource reads AVR frames from a local file at a configurable rate.
// It is used for deterministic testing and benchmarking without RF hardware.
type ReplaySource struct {
	path     string
	interval time.Duration // 0 = maximum throughput
}

// NewReplaySource creates a ReplaySource reading from path.
// If interval is zero, frames are delivered at maximum throughput.
func NewReplaySource(path string, interval time.Duration) *ReplaySource {
	return &ReplaySource{path: path, interval: interval}
}

// Run reads all AVR lines from r.path, sending validated RawFrames to out.
// Delivery is paced: each valid frame is followed by a sleep of r.interval
// (skipped when interval is zero).
//
// Unlike TCPSource, replay uses blocking sends to preserve determinism — no
// frame from the fixture file is silently dropped. If out is full, Run waits.
//
// Returns nil on clean EOF, or ctx.Err() if ctx is canceled before EOF.
func (r *ReplaySource) Run(ctx context.Context, out chan<- RawFrame, metrics *Metrics) error {
	f, err := os.Open(r.path)
	if err != nil {
		return fmt.Errorf("ingest/replay: open %q: %w", r.path, err)
	}
	defer f.Close()

	log.Printf("ingest/replay: reading %s (interval=%v)", r.path, r.interval)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, maxScannerBufBytes), maxScannerBufBytes)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		metrics.TotalFrames.Add(1)
		hex, ok := ParseFrame(scanner.Text())
		if !ok {
			metrics.MalformedFrames.Add(1)
			continue
		}

		frame := RawFrame{Hex: hex, ReceivedAt: time.Now()}

		// Send the frame before pacing so the first frame is delivered immediately.
		if !sendFrameBlocking(ctx, frame, out) {
			return ctx.Err()
		}

		// Pace delivery: sleep after each valid frame (skipped at interval==0).
		if r.interval > 0 {
			if !sleepCtx(ctx, r.interval) {
				return ctx.Err()
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("ingest/replay: scan error: %w", err)
	}

	log.Printf("ingest/replay: finished %s — total=%d malformed=%d",
		r.path, metrics.TotalFrames.Load(), metrics.MalformedFrames.Load())
	return nil
}
