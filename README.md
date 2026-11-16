# SDR Aircraft Tracker

A high-performance, real-time Mode S / ADS-B decoding, Compact Position Reporting (CPR) tracking, and terminal visualization system implemented natively in Go.

```text
 [RTL-SDR USB (1090 MHz)]
          │ (Raw IQ Samples via USB)
          ▼
   [dump1090 RF Front-End]            [Deterministic Replay File]
   (*8D4CA2...; on TCP :30002)            (*.avr fixture stream)
          │                                         │
          └───────────────────┬─────────────────────┘
                              ▼
           ┌─────────────────────────────────────┐
           │      Ingester (internal/ingest)     │
           │  - Line framing & hex validation    │
           │  - Exponential backoff TCP client   │
           │  - Non-blocking bounded frame queue │
           └──────────────────┬──────────────────┘
                              ▼
           ┌─────────────────────────────────────┐
           │       Decoder (internal/adsb)       │
           │  - 0-alloc Mode S CRC-24 verifier   │
           │  - DF17 bitwise field extraction    │
           │  - Global Airborne CPR solver       │
           └──────────────────┬──────────────────┘
                              ▼
           ┌─────────────────────────────────────┐
           │     State Engine (internal/tracker) │
           │  - Thread-safe ICAO repository      │
           │  - Even/Odd CPR time-window pairing │
           │  - Kinematic plausibility filters   │
           │  - Background TTL inactivity reaper │
           └──────────────┬───────────────┬──────┘
                          │ (Snapshot)    │ (JSON slog)
                          ▼               ▼
              ┌──────────────────┐ ┌─────────────┐
              │  Bubble Tea TUI  │ │ Structured  │
              │  (internal/tui)  │ │ JSON Logs   │
              └──────────────────┘ └─────────────┘
```

---

## Architectural Boundary & Original Contributions

**SDR Aircraft Tracker** follows a clean architectural separation of concerns:

1. **RF Front-End (`dump1090`):** Samples 1090 MHz RF over USB via `librtlsdr`, performs 2 MHz baseband filtering and pulse-position modulation (PPM) detection, emitting raw ASCII AVR hexadecimal frames (`*<HEX>;\n`) over TCP port `30002`.
2. **Native Go Processing (100% Original Implementation):**
   * **Independent Mode S CRC-24 Engine:** Bitwise and table-driven polynomial division ($G(x) = \text{0xFFF409}$) running with zero heap allocations on 112-bit frames.
   * **Stateless DF17 Bitwise Decoder:** Extraction of 24-bit ICAO identifiers, 8-character AIS callsigns (TC 1–4), 25-ft and Gillham Gray-code barometric altitudes (TC 9–18), and subsonic/supersonic velocity vectors (TC 19).
   * **Global Compact Position Reporting (CPR) Solver:** Implementation of ICAO Doc 9871 17-bit latitude zone ($N_z = 15$) and longitude resolution across Even and Odd frame pairs with strict zone consistency verification ($NL(\text{lat}_{\text{even}}) == NL(\text{lat}_{\text{odd}})$).
   * **Concurrent Kinematic State Engine:** Thread-safe state tracking protected by `sync.RWMutex`, enforcing $\le 10\text{s}$ Even/Odd correlation windows, speed limit checks ($\le 1200\text{ kt}$), and great-circle Haversine jump filtering to prevent telemetry anomalies.
   * **Interactive Terminal User Interface:** Responsive, flicker-free dashboard built on Charm's Bubble Tea and Lipgloss, decoupled from packet ingestion via snapshot polling.
   * **Lifecycle Logging:** Optional synchronous JSON startup/shutdown logs via Go's `log/slog`; decoded aircraft archival is not implemented.

---

## Measured Performance Benchmarks

Microbenchmarks rerun during the audit on Linux amd64, AMD Ryzen 7 7700,
Go 1.27.3, with `go test -run '^$' -bench=. -benchtime=100ms -benchmem ./internal/adsb ./internal/tracker`:

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| CRC24 | 5.759 | 0 | 0 |
| CRC24Bitwise | 119.1 | 0 | 0 |
| DecodeDF17 (position frame) | 8.962 | 0 | 0 |
| ProcessRawHex (position frame) | 40.25 | 0 | 0 |
| GlobalCPR | 74.72 | 0 | 0 |
| TrackerUpdate (existing aircraft) | 9.196 | 0 | 0 |
| TrackerSnapshot (100 aircraft) | 2592 | 27264 | 1 |

These are isolated microbenchmarks, not end-to-end throughput measurements.
Identification decoding and first insertion can allocate. Results vary by host.

---

## Installation & Build

### Prerequisites
* Go: module minimum 1.22; audit executed with 1.27.3 (`go version`). CI checks the module minimum and stable Go.
* Git for cloning; a C compiler for `go test -race` (normal static builds do not need one).
* An interactive terminal for the TUI; use `--headless` in scripts.
* Tested here on Pop!_OS 24.04, Linux amd64. Other platforms are unverified.
* *(Optional for live reception)* RTL-SDR USB dongle and `dump1090` (`sudo apt install dump1090-mutability` or `dump1090-fa`)

### Compiling
```bash
git clone https://github.com/khalid7344/sdr-aircraft-tracker.git
cd sdr-aircraft-tracker
go build -o bin/sdr-tracker ./cmd/sdr-tracker
```

---

## Usage

### 1. Deterministic Replay Mode (No SDR Hardware Required)
Replays recorded Mode S AVR frames at configurable pacing:

```bash
# Maximum throughput in headless mode:
./bin/sdr-tracker --mode=replay --replay-file=testdata/sample_frames.avr --replay-interval=0 --headless

# Paced replay (50ms interval) with interactive TUI:
./bin/sdr-tracker --mode=replay --replay-file=testdata/sample_frames.avr --replay-interval=50ms

# Replay with JSON lifecycle logging:
./bin/sdr-tracker --mode=replay --replay-file=testdata/sample_frames.avr --log-file=replay.jsonl
```

The checked-in fixture produces 9 lines: 4 valid DF17, 2 other frames, 3 malformed,
1 CPR position, and no dropped frames. CRC rejection is covered by unit tests.
A missing replay file returns a nonzero exit status.

See the genuine [TUI terminal recording](docs/demo/replay.typescript) and
[recording instructions](docs/demo/README.md). It uses reference frames, not live RF.

### 2. Live RTL-SDR Tracking Mode
Connects to a running `dump1090` instance listening on port 30002. Install and
configure your chosen demodulator separately, including its RTL-SDR udev rules.
Package names and options vary; check its `--help`. The command below is an
unvalidated example, not a hardware validation result:

```bash
# Terminal 1: Start RF demodulator
dump1090 --net --net-ro-port 30002

# Terminal 2: Run SDR Aircraft Tracker
./bin/sdr-tracker --mode=tcp --addr=127.0.0.1:30002 --log-file=flight_telemetry.jsonl
```

### CLI Flags
```text
Usage of sdr-tracker:
  -mode string
        Ingestion mode: "tcp" or "replay" (default "tcp")
  -addr string
        dump1090 AVR raw TCP address (default "127.0.0.1:30002")
  -replay-file string
        Path to recorded AVR hex frame file for replay mode (default "testdata/sample_frames.avr")
  -replay-interval duration
        Delay between replayed frames; 0 runs at maximum throughput (default 10ms)
  -ttl duration
        Aircraft inactivity eviction timeout (default 1m0s)
  -log-file string
        Optional file path for structured JSON slog output (default "")
  -headless
        Run without Bubble Tea TUI and output JSON summary on exit (default false)
```

### Interactive TUI Keybindings
* `a`: Sort aircraft table by Altitude (descending)
* `m`: Sort aircraft table by Message Count (descending)
* `s`: Sort aircraft table by Last Seen recency
* `q` / `Ctrl+C`: Clean exit and terminal restoration

---

## Testing & Verification

Run the full automated test suite with race detection enabled:

```bash
# Check code formatting
gofmt -s -l .

# Run static analysis
go vet ./...

# Run all unit and integration tests with Go race detector
go test -v -race ./...

# Run microbenchmarks
go test -bench=. -benchmem ./internal/adsb ./internal/tracker
```

---

## Mathematical Principles & ADS-B Theory

### Mode S CRC-24 Parity
Mode S Extended Squitter transmissions protect 88 payload bits with a 24-bit Cyclic Redundancy Check using generator polynomial:
$$G(x) = x^{24} + x^{23} + x^{22} + x^{21} + x^{20} + x^{19} + x^{18} + x^{17} + x^{16} + x^{15} + x^{14} + x^{13} + x^{12} + x^{10} + x^3 + 1$$
In hexadecimal, this is represented as `0xFFF409`. When dividing the entire 112-bit message (payload + parity) over Galois Field $GF(2)$, the remainder for an uncorrupted DF17 frame must evaluate to exactly `0x000000`.

### Compact Position Reporting (CPR)
ADS-B compresses 360° spherical coordinates into 17-bit integers by dividing latitude into 60 Even zones ($6.0^\circ$) and 59 Odd zones ($6.1017^\circ$), analogous to a Vernier scale. A global coordinate is resolved when an aircraft transmits both an Even and Odd frame within a strict 10-second window:
1. Latitude index $j = \lfloor 59 \cdot Y_{\text{even}} - 60 \cdot Y_{\text{odd}} + 0.5 \rfloor$
2. Recovered latitudes $\text{lat}_{\text{even}}$ and $\text{lat}_{\text{odd}}$ are checked against zone transitions: $NL(\text{lat}_{\text{even}}) == NL(\text{lat}_{\text{odd}})$.
3. Longitude index $m = \lfloor X_{\text{even}} \cdot (NL - 1) - X_{\text{odd}} \cdot NL + 0.5 \rfloor$
4. Recovered coordinates are filtered through kinematic plausibility bounds before updating state.

---

## Known Limitations & Validation Status

* The audit environment forbids listening sockets: `TestTCPSourceReconnect` fails
  with `operation not permitted`. Other tests pass with race detection. Keep the
  complete test suite enabled in CI and rerun on an unrestricted Linux host.
* USB access could not be initialized here. No hardware capture is available.
* Logs contain lifecycle events only; asynchronous logging and per-aircraft JSON
  archival remain roadmap items. TCP diagnostics currently go to stderr.
* Replay uses arrival timestamps and blocking delivery; TCP drops frames when the
  bounded queue fills. An overlong input line terminates replay or reconnects TCP.
* This is an educational tracker, unsuitable for safety-critical navigation.

## Security

The tracker is an outbound TCP client and requires no root privileges. Trust the
configured upstream; TCP is unencrypted. Logs and captures can reveal aircraft
locations and observation patterns. New log files use mode 0600; existing file
permissions are preserved. Avoid committing private captures.

## Release strategy

The existing local `v1.0.0` tag is preserved. These repairs are proposed for
`v1.0.1`; no tag or GitHub release has been published by this audit. See
[release notes](docs/RELEASE_NOTES_v1.0.1.md). Only Linux amd64 artifacts will be
prepared, with SHA-256 checksums. Hardware and remote CI verification remain pending.

* **Hardware Validation Status:** `PENDING HARDWARE RUN`. All protocol mechanics, frame parsing, CRC verification, and CPR state transitions are verified via deterministic test fixtures. Live USB RTL-SDR reception requires physical dongle connection (`0bda:2838`) and active airspace line-of-sight.
* **Surface Position (TC 5–8):** Not implemented; MVP focuses strictly on Airborne Position (TC 9–18) and Velocity (TC 19).
* **Single-Frame Local CPR:** Excluded to maintain deterministic spatial mathematics requiring Even/Odd pairing.

---

## License

MIT License. See [LICENSE](LICENSE) for details.
