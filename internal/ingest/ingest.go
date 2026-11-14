// Package ingest provides AVR frame ingestion from TCP and file-replay sources.
//
// Architecture: the ingest layer is responsible only for line framing and
// channel delivery. CRC-24 validation and DF17 decoding are handled in M2
// (internal/adsb). State correlation is handled in M3 (internal/tracker).
package ingest

import (
	"context"
	"strings"
	"sync/atomic"
	"time"
)

// maxScannerBufBytes caps the per-line buffer used by both TCP and replay
// scanners. This prevents memory exhaustion from malformed streams that contain
// no newline character. 300 bytes is well above the longest valid AVR line
// (31 bytes: '*' + 28 hex + ';' + '\n') and well below any alarming limit.
const maxScannerBufBytes = 300

// RawFrame represents a single stripped hex string received from TCP or a replay file.
// Hex is normalised to upper-case; it has not yet been CRC-validated or decoded.
type RawFrame struct {
	Hex        string
	ReceivedAt time.Time
}

// Metrics holds live atomic pipeline counters that are safe for concurrent access.
// Fields are exported so main.go and TUI can read them.
type Metrics struct {
	TotalFrames         atomic.Uint64
	MalformedFrames     atomic.Uint64
	NonDF17Frames       atomic.Uint64
	CRCFailedFrames     atomic.Uint64
	ValidDF17Frames     atomic.Uint64
	CPRPositionsSolved  atomic.Uint64
	PlausibilityRejects atomic.Uint64
	DroppedFrames       atomic.Uint64
	ReconnectCount      atomic.Uint64
	Connected           atomic.Bool
}

// Source is implemented by TCPSource and ReplaySource.
type Source interface {
	// Run reads frames until ctx is canceled or the source is exhausted (replay EOF).
	// Validated RawFrames are written to out; Metrics are updated atomically.
	// Returns nil on clean EOF (replay only) or ctx.Err() on cancellation.
	Run(ctx context.Context, out chan<- RawFrame, metrics *Metrics) error
}

// ParseFrame validates an AVR-formatted line and returns the stripped,
// upper-cased hex string on success.
//
// Acceptance criteria (PROJECT.md FR-01):
//   - Total line length after stripping trailing CR/LF must not exceed 256 bytes.
//   - Line must begin with '*' and end with ';'.
//   - The hex payload between the delimiters must be exactly 14 or 28 characters.
//   - Every character in the hex payload must be a valid hexadecimal digit [0-9A-Fa-f].
//
// Returns ("", false) for any malformed input.
func ParseFrame(line string) (string, bool) {
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 || len(line) > 256 {
		return "", false
	}
	if line[0] != '*' || line[len(line)-1] != ';' {
		return "", false
	}
	hex := line[1 : len(line)-1]
	if len(hex) != 14 && len(hex) != 28 {
		return "", false
	}
	if !isValidHex(hex) {
		return "", false
	}
	return strings.ToUpper(hex), true
}

// sendFrame sends frame to out without blocking (used by TCPSource).
// If out is full or ctx is canceled the frame is silently dropped and
// m.DroppedFrames is incremented. This preserves low-latency real-time
// tracking: stale RF frames are discarded rather than stalling TCP reads.
func sendFrame(ctx context.Context, frame RawFrame, out chan<- RawFrame, m *Metrics) {
	select {
	case out <- frame:
	case <-ctx.Done():
	default:
		m.DroppedFrames.Add(1)
	}
}

// sendFrameBlocking sends frame to out, blocking until the frame is accepted
// or ctx is canceled (used by ReplaySource to preserve deterministic delivery).
// Returns false if ctx was canceled before the frame could be sent.
func sendFrameBlocking(ctx context.Context, frame RawFrame, out chan<- RawFrame) bool {
	select {
	case out <- frame:
		return true
	case <-ctx.Done():
		return false
	}
}

// isValidHex reports whether every byte of s is a valid hexadecimal character.
func isValidHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// sleepCtx sleeps for d or until ctx is canceled.
// Returns false when ctx was canceled before d elapsed.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// capDuration returns a if a < b, otherwise b.
func capDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
