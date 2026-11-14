package ingest

import (
	"bufio"
	"context"
	"log"
	"net"
	"time"
)

// TCPSource connects to a dump1090 AVR TCP endpoint and streams raw frames.
// On connection failure or disconnect it retries with capped exponential backoff.
type TCPSource struct {
	addr           string
	initialBackoff time.Duration
	maxBackoff     time.Duration
}

// NewTCPSource creates a TCPSource targeting addr with production backoff defaults
// (500 ms initial, 10 s maximum) as specified in PROJECT.md FR-02.
func NewTCPSource(addr string) *TCPSource {
	return &TCPSource{
		addr:           addr,
		initialBackoff: 500 * time.Millisecond,
		maxBackoff:     10 * time.Second,
	}
}

// Run connects to t.addr and streams AVR frames into out until ctx is canceled.
// On connection error or disconnect, it logs a structured warning and retries
// with capped exponential backoff. metrics.ReconnectCount is incremented on
// each retry attempt (after the first connection attempt).
func (t *TCPSource) Run(ctx context.Context, out chan<- RawFrame, metrics *Metrics) error {
	backoff := t.initialBackoff
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", t.addr)
		if err != nil {
			metrics.Connected.Store(false)
			log.Printf("ingest/tcp: connect error (retry in %v): %v", backoff, err)
			if !sleepCtx(ctx, backoff) {
				return ctx.Err()
			}
			backoff = capDuration(backoff*2, t.maxBackoff)
			metrics.ReconnectCount.Add(1)
			continue
		}

		metrics.Connected.Store(true)
		log.Printf("ingest/tcp: connected to %s", t.addr)
		backoff = t.initialBackoff // reset on successful connection

		t.readConn(ctx, conn, out, metrics)
		conn.Close()
		metrics.Connected.Store(false)

		if ctx.Err() != nil {
			return ctx.Err()
		}

		log.Printf("ingest/tcp: connection lost (retry in %v)", backoff)
		if !sleepCtx(ctx, backoff) {
			return ctx.Err()
		}
		backoff = capDuration(backoff*2, t.maxBackoff)
		metrics.ReconnectCount.Add(1)
	}
}

// readConn reads AVR lines from conn until it disconnects or ctx is canceled.
// It caps per-line memory at maxScannerBufBytes to prevent memory exhaustion
// from malformed streams without newlines (PROJECT.md §5).
func (t *TCPSource) readConn(ctx context.Context, conn net.Conn, out chan<- RawFrame, metrics *Metrics) {
	// Goroutine closes conn when ctx is done so the blocking scanner.Scan()
	// unblocks. The done channel prevents the goroutine from outliving readConn.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, maxScannerBufBytes), maxScannerBufBytes)

	for scanner.Scan() {
		metrics.TotalFrames.Add(1)
		hex, ok := ParseFrame(scanner.Text())
		if !ok {
			metrics.MalformedFrames.Add(1)
			continue
		}
		frame := RawFrame{Hex: hex, ReceivedAt: time.Now()}
		sendFrame(ctx, frame, out, metrics)
	}
	// scanner.Err() == nil on clean EOF or conn.Close(); non-nil on scanner
	// buffer overflow (ErrTooLong). Either way, Run's loop will reconnect.
}
