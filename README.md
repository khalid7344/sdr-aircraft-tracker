# SDR Aircraft Tracker

[![CI](https://github.com/khalid7344/sdr-aircraft-tracker/actions/workflows/ci.yml/badge.svg)](https://github.com/khalid7344/sdr-aircraft-tracker/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

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
                          │ (Snapshot)    │ (CLI lifecycle JSON)
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
2. **Native Go Processing:**
   * **Independent Mode S CRC-24 Engine:** Bitwise and table-driven polynomial division (lower 24 polynomial coefficients: `0xFFF409`) running with zero heap allocations on 112-bit frames.
   * **Stateless DF17 Bitwise Decoder:** Extraction of 24-bit ICAO identifiers, 8-character AIS callsigns (TC 1–4), 25-ft and Gillham Gray-code barometric altitudes (TC 9–18), and subsonic/supersonic velocity vectors (TC 19).
   * **Global Compact Position Reporting (CPR) Solver:** Implementation of ICAO Doc 9871 17-bit CPR coordinates with latitude-zone parameter `N_z = 15` and longitude resolution across Even and Odd frame pairs with strict zone consistency verification (`NL(lat_even) == NL(lat_odd)`).
   * **Concurrent Kinematic State Engine:** Thread-safe state tracking protected by `sync.RWMutex`, enforcing at most 10 seconds Even/Odd correlation windows, speed limit checks (at most 1200 knots), and great-circle Haversine jump filtering to prevent telemetry anomalies.
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

## Linux amd64 release

Download [v1.0.1](https://github.com/khalid7344/sdr-aircraft-tracker/releases/tag/v1.0.1):
[Linux amd64 archive](https://github.com/khalid7344/sdr-aircraft-tracker/releases/download/v1.0.1/sdr-aircraft-tracker_v1.0.1_linux_amd64.tar.gz)
and [SHA256SUMS](https://github.com/khalid7344/sdr-aircraft-tracker/releases/download/v1.0.1/SHA256SUMS).
Place both files in the same directory, then verify before extracting:

```sh
sha256sum -c SHA256SUMS
tar -xzf sdr-aircraft-tracker_v1.0.1_linux_amd64.tar.gz
cd sdr-aircraft-tracker_v1.0.1_linux_amd64
./sdr-tracker --version
./sdr-tracker --mode=replay --replay-file=testdata/sample_frames.avr --replay-interval=0 --headless
```

The archive includes the replay fixture, documentation, project license, dependency
notices and build provenance. It is static Linux amd64, tested on the audit host;
no other platform compatibility or physical RTL-SDR validation is asserted.

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
  -version
        Print version and source commit, then exit
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

### Mode S CRC-24 parity

An Extended Squitter frame contains 88 payload bits and 24 parity bits. The Mode S
generator polynomial is written below with ASCII exponents:

```text
G(x) = x^24 + x^23 + x^22 + x^21 + x^20 + x^19 + x^18 + x^17
     + x^16 + x^15 + x^14 + x^13 + x^12 + x^10 + x^3 + 1
```

Its lower 24 coefficients are `0xFFF409`; the leading `x^24` term is implicit in
that notation. The full 25-bit polynomial is `0x1FFF409`. Division uses `GF(2)`:
addition and subtraction are XOR. A valid DF17 frame has a zero CRC remainder.

### Compact Position Reporting (CPR)

CPR encodes latitude and longitude as 17-bit integers. With `N_z = 15`, there are
60 even latitude zones and 59 odd zones. The tracker needs an even/odd pair received
at most 10 seconds apart; the more recent frame determines the final position.

Let `Y_even`, `Y_odd`, `X_even` and `X_odd` be encoded latitude/longitude values
divided by `131072` (2^17). `mod` below is nonnegative modulo, and `floor` rounds
downward:

```text
j = floor(59 * Y_even - 60 * Y_odd + 0.5)
lat_even = (360 / 60) * (mod(j, 60) + Y_even)
lat_odd  = (360 / 59) * (mod(j, 59) + Y_odd)
```

Latitudes at least 270 degrees are reduced by 360 degrees to recover southern
coordinates; results outside -90..90 degrees are rejected. Both frames must have
matching longitude-zone counts: `NL(lat_even) == NL(lat_odd)`.

```text
NL = NL(lat_even)  # equal to NL(lat_odd) for an accepted pair
m = floor(X_even * (NL - 1) - X_odd * NL + 0.5)
```

Longitude uses `360 / max(NL - parity, 1)` degrees per zone, with `parity = 0` for
even or `1` for odd, and the corresponding `X` value. The result is normalized to
-180..180 degrees. The tracker applies coordinate, altitude, speed and movement
plausibility checks before accepting updates.

---

## Validation and limitations

- **Software:** deterministic reference-frame replay, CRC rejection, CPR pairing,
  state tracking and TUI behavior have automated tests. [CI](https://github.com/khalid7344/sdr-aircraft-tracker/actions/workflows/ci.yml)
  runs the full race suite on the module minimum and stable Go. Check the workflow
  for the latest result. The tests require permission to open a loopback TCP listener.
- **Hardware:** live RTL-SDR USB reception has **not been validated**. No live
  capture is bundled. Fixture replay does not establish RF reception quality,
  antenna performance or demodulator compatibility.
- **Environment:** tested locally on Pop!_OS 24.04, Linux amd64. Other Linux
  distributions and architectures are unverified; the downloadable binary is Linux
  amd64 only.
- **Protocol:** surface positions (TC 5-8), single-frame local CPR, MLAT, and deep
  decoding of non-DF17 messages are not implemented.
- **Logging:** JSON logs contain lifecycle events only. Asynchronous logging and
  per-aircraft archival remain future work; TCP diagnostics currently use stderr.
- **Input:** replay uses arrival timestamps and blocking delivery. TCP drops frames
  when the bounded queue fills. An overlong line stops replay or reconnects TCP.
- **Use:** this is an educational tracker, unsuitable for safety-critical navigation.

## Security

The tracker is an outbound TCP client and requires no root privileges. Trust the
configured upstream; TCP is unencrypted. Logs and captures can reveal aircraft
locations and observation patterns. New log files use mode 0600; existing file
permissions are preserved. Avoid committing private captures.

## Releases

[v1.0.1](https://github.com/khalid7344/sdr-aircraft-tracker/releases/tag/v1.0.1)
contains the protocol and CLI repairs described in the [release notes](docs/RELEASE_NOTES_v1.0.1.md).
The archive identifies source commit `95f100c666ad585e06b52d09230813aef50bb7a7`
through `--version` and `BUILD_INFO.json`. It includes the replay fixture, project
license and dependency notices. Check `SHA256SUMS` before running it. Later
README changes on `main` do not replace the immutable v1.0.1 archive or tag.

---

## License

MIT License. See [LICENSE](LICENSE) for details.

## Troubleshooting

- **No terminal:** use `--headless`; interactive mode needs a real terminal.
- **Missing replay file:** run from the project/extracted archive root and pass the
  fixture path shown above. The command exits nonzero when the source cannot open.
- **No live aircraft:** check your demodulator's raw AVR TCP output and antenna;
  the Go process does not access USB directly. Hardware reception is unvalidated.
- **TCP connection refused:** confirm the configured address and upstream raw port.
  The client retries with bounded backoff until canceled.
- **Source archive build lacks Git metadata:** add `-buildvcs=false` to `go build`.
