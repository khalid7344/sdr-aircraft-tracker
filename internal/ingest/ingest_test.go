// Tests for internal/ingest: frame parsing, replay EOF, TCP reconnect, and
// bounded channel backpressure. All tests run in package ingest (white-box)
// so they can configure unexported fields such as TCPSource.initialBackoff
// for faster test execution without altering production defaults.
package ingest

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// ParseFrame — valid inputs
// ---------------------------------------------------------------------------

func TestParseFrameValid(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{
			name: "DF17_KLM1023",
			line: "*8D4840D6202CC371C32CE0576098;\n",
			want: "8D4840D6202CC371C32CE0576098",
		},
		{
			name: "DF17_even_position",
			line: "*8D40621D58C382D690C8AC2863A7;\n",
			want: "8D40621D58C382D690C8AC2863A7",
		},
		{
			name: "DF17_velocity",
			line: "*8D485020994409940838175B284F;\n",
			want: "8D485020994409940838175B284F",
		},
		{
			name: "short_14_char_frame",
			line: "*02E197B54848A6;\n",
			want: "02E197B54848A6",
		},
		{
			name: "lowercase_hex_uppercased",
			line: "*8d4840d6202cc371c32ce0576098;\n",
			want: "8D4840D6202CC371C32CE0576098",
		},
		{
			name: "no_trailing_newline",
			line: "*8D4840D6202CC371C32CE0576098;",
			want: "8D4840D6202CC371C32CE0576098",
		},
		{
			name: "crlf_line_ending",
			line: "*8D4840D6202CC371C32CE0576098;\r\n",
			want: "8D4840D6202CC371C32CE0576098",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ParseFrame(c.line)
			if !ok {
				t.Fatalf("ParseFrame returned ok=false, want true")
			}
			if got != c.want {
				t.Errorf("hex = %q, want %q", got, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ParseFrame — malformed inputs (must all return ok=false)
// ---------------------------------------------------------------------------

func TestParseFrameMalformed(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{name: "empty_string", line: ""},
		{name: "whitespace_only", line: "   "},
		{name: "missing_leading_star", line: "8D4840D6202CC371C32CE0576098;"},
		{name: "missing_trailing_semicolon", line: "*8D4840D6202CC371C32CE0576098"},
		{name: "hex_26_chars_not_14_or_28", line: "*8D40621D58C382D690C8AC2863;"},
		{name: "hex_12_chars_not_14_or_28", line: "*8D4840D6202C;"},
		{name: "empty_between_delimiters", line: "*;"},
		{name: "non_hex_chars", line: "*ZZZZZZZZZZZZZZZZZZZZZZZZZZZZ;"},
		{name: "hex_with_space", line: "*8D4840D6 202CC371C32CE0576098;"},
		{name: "exceeds_256_bytes", line: "*" + strings.Repeat("A", 260) + ";"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, ok := ParseFrame(c.line)
			if ok {
				t.Errorf("ParseFrame(%q) = ok=true, want false", c.line)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Replay source — valid file, EOF, cancellation, malformed lines
// ---------------------------------------------------------------------------

// writeTempAVR creates a temporary AVR fixture file and returns its path.
// The file and its parent directory are cleaned up automatically by t.Cleanup.
func writeTempAVR(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "test*.avr")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

func TestReplaySourceValidFrames(t *testing.T) {
	content := "*8D4840D6202CC371C32CE0576098;\n" +
		"*8D40621D58C382D690C8AC2863A7;\n" +
		"*8D485020994409940838175B284F;\n"
	path := writeTempAVR(t, content)

	src := NewReplaySource(path, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out := make(chan RawFrame, 10)
	metrics := &Metrics{}

	err := src.Run(ctx, out, metrics)
	if err != nil {
		t.Fatalf("Run returned error on clean EOF: %v", err)
	}
	if got := metrics.TotalFrames.Load(); got != 3 {
		t.Errorf("TotalFrames = %d, want 3", got)
	}
	if got := metrics.MalformedFrames.Load(); got != 0 {
		t.Errorf("MalformedFrames = %d, want 0", got)
	}
	if got := len(out); got != 3 {
		t.Errorf("frames in channel = %d, want 3", got)
	}
}

func TestReplaySourceMalformedLines(t *testing.T) {
	// 2 valid, 3 malformed (no star, wrong length, non-hex)
	content := "*8D4840D6202CC371C32CE0576098;\n" +
		"8D4840D6202CC371C32CE0576098;\n" + // no leading *
		"*8D40621D58C382D690C8AC2863;\n" + // 26 chars (not 14 or 28)
		"*ZZZZZZZZZZZZZZZZZZZZZZZZZZZZ;\n" + // non-hex
		"*8D40621D58C382D690C8AC2863A7;\n"
	path := writeTempAVR(t, content)

	src := NewReplaySource(path, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out := make(chan RawFrame, 10)
	metrics := &Metrics{}

	err := src.Run(ctx, out, metrics)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := metrics.TotalFrames.Load(); got != 5 {
		t.Errorf("TotalFrames = %d, want 5", got)
	}
	if got := metrics.MalformedFrames.Load(); got != 3 {
		t.Errorf("MalformedFrames = %d, want 3", got)
	}
	if got := len(out); got != 2 {
		t.Errorf("frames in channel = %d, want 2", got)
	}
}

func TestReplaySourceEOFReturnValue(t *testing.T) {
	// Run must return nil (not an error) on clean EOF.
	path := writeTempAVR(t, "*8D4840D6202CC371C32CE0576098;\n")
	src := NewReplaySource(path, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := src.Run(ctx, make(chan RawFrame, 10), &Metrics{})
	if err != nil {
		t.Fatalf("Run returned %v on EOF, want nil", err)
	}
}

func TestReplaySourceCancellation(t *testing.T) {
	// 500 frames at 10 ms each = 5 seconds of replay.
	// Cancel after 50 ms; Run must return a context error promptly.
	content := strings.Repeat("*8D4840D6202CC371C32CE0576098;\n", 500)
	path := writeTempAVR(t, content)

	src := NewReplaySource(path, 10*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	out := make(chan RawFrame, 1024)
	err := src.Run(ctx, out, &Metrics{})
	if err == nil {
		t.Fatal("Run returned nil after cancellation, want context error")
	}
}

// ---------------------------------------------------------------------------
// TCP source — reconnect after server disconnect
// ---------------------------------------------------------------------------

func TestTCPSourceReconnect(t *testing.T) {
	const testLine = "*8D4840D6202CC371C32CE0576098;\n"
	const wantHex = "8D4840D6202CC371C32CE0576098"
	const wantConnections = 2

	// In-process listener; OS assigns port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// Server: accept wantConnections connections, write one frame each, then close.
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		for i := 0; i < wantConnections; i++ {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Write([]byte(testLine)) //nolint:errcheck
			conn.Close()
		}
	}()

	// Use accelerated backoff so the test completes quickly.
	src := &TCPSource{
		addr:           ln.Addr().String(),
		initialBackoff: 20 * time.Millisecond,
		maxBackoff:     100 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out := make(chan RawFrame, 10)
	metrics := &Metrics{}

	go func() {
		src.Run(ctx, out, metrics) //nolint:errcheck
	}()

	// Expect one frame per server connection.
	for i := 0; i < wantConnections; i++ {
		select {
		case frame := <-out:
			if frame.Hex != wantHex {
				t.Errorf("connection %d: hex = %q, want %q", i+1, frame.Hex, wantHex)
			}
		case <-ctx.Done():
			t.Fatalf("timeout waiting for frame %d (reconnect may not have fired)", i+1)
		}
	}

	// Wait for the server to finish so we can safely check reconnect count.
	select {
	case <-serverDone:
	case <-ctx.Done():
		t.Fatal("server goroutine did not finish in time")
	}

	if got := metrics.ReconnectCount.Load(); got == 0 {
		t.Errorf("ReconnectCount = 0, want >= 1 (second connection was a reconnect)")
	}

	cancel()
}

// ---------------------------------------------------------------------------
// Bounded channel — non-blocking drop under backpressure (TCP mode)
// ---------------------------------------------------------------------------

func TestBoundedChannelDrop(t *testing.T) {
	// Channel capacity of 2: exactly fills after two frames.
	out := make(chan RawFrame, 2)
	metrics := &Metrics{}
	ctx := context.Background()

	frame := RawFrame{Hex: "8D4840D6202CC371C32CE0576098", ReceivedAt: time.Now()}

	// Two sends must succeed (channel has space).
	sendFrame(ctx, frame, out, metrics)
	sendFrame(ctx, frame, out, metrics)

	if got := metrics.DroppedFrames.Load(); got != 0 {
		t.Fatalf("unexpected drops before channel full: got %d", got)
	}

	// Two sends must drop without blocking (channel full).
	sendFrame(ctx, frame, out, metrics)
	sendFrame(ctx, frame, out, metrics)

	if got := metrics.DroppedFrames.Load(); got != 2 {
		t.Errorf("DroppedFrames = %d, want 2", got)
	}
	if got := len(out); got != 2 {
		t.Errorf("channel len = %d, want 2 (no extra frames written)", got)
	}
}

// ---------------------------------------------------------------------------
// Metrics counter — basic atomicity smoke test
// ---------------------------------------------------------------------------

func TestMetricsAtomicOperations(t *testing.T) {
	m := &Metrics{}
	m.TotalFrames.Add(100)
	m.MalformedFrames.Add(5)
	m.DroppedFrames.Add(3)
	m.ReconnectCount.Add(2)
	m.Connected.Store(true)

	if got := m.TotalFrames.Load(); got != 100 {
		t.Errorf("TotalFrames = %d, want 100", got)
	}
	if got := m.MalformedFrames.Load(); got != 5 {
		t.Errorf("MalformedFrames = %d, want 5", got)
	}
	if got := m.DroppedFrames.Load(); got != 3 {
		t.Errorf("DroppedFrames = %d, want 3", got)
	}
	if got := m.ReconnectCount.Load(); got != 2 {
		t.Errorf("ReconnectCount = %d, want 2", got)
	}
	if !m.Connected.Load() {
		t.Error("Connected = false, want true")
	}
}

// net.Pipe verifies connection reads and cancellation without opening a listener.
func TestReadConnPipeCancellation(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := NewTCPSource("unused")
	out := make(chan RawFrame, 1)
	metrics := &Metrics{}
	done := make(chan struct{})
	go func() { defer close(done); source.readConn(ctx, client, out, metrics) }()
	if _, err := server.Write([]byte("*8D4840D6202CC371C32CE0576098;\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-out:
		if f.Hex != "8D4840D6202CC371C32CE0576098" {
			t.Fatal(f.Hex)
		}
	case <-time.After(time.Second):
		t.Fatal("frame was not read")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("connection did not cancel")
	}
}
