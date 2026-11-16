# SDR engineering audit

## Repairs

- Corrected repository clone URL and relative license link.
- Corrected Gillham altitude bit ordering, invalid states, direction and offset;
  corrected Q=1 minimum altitude, polar NL boundary, CPR input bounds, and malformed
  short hexadecimal classification. Regression tests cover these cases.
- CLI rejects invalid timing; replay errors and TUI errors return nonzero.
  Shutdown cancels ingestion and waits for decoder completion before closing logs.
- New log files use 0600. Current JSON logs remain synchronous lifecycle events;
  per-aircraft archival and asynchronous logging are not implemented.
- Added CLI replay/error tests and a socket-free `net.Pipe` read/cancellation test.
- CI checks minimum and stable Go, formatting, vet, module integrity, races, all
  packages, and static binary compilation.
- Replaced unsupported throughput extrapolation with measured microbenchmarks.
  Added genuine reference-frame TUI recording and replay JSON output.

Gillham encoding was cross-checked against the named bit layout and mathematical
conversion described in [pyModeS's altitude decoder](https://github.com/junzis/pyModeS/blob/main/src/pyModeS/_altcode.py).
No external decoding package was added.

## Test results

`go test -race -count=1 ./...` was run and fails only at
`TestTCPSourceReconnect`: sandbox policy forbids `listen tcp 127.0.0.1:0`.
`go test -race -skip '^TestTCPSourceReconnect$' -count=1 ./...` passes every
remaining test, including CLI replay and `net.Pipe`. The failing test remains
unchanged and enabled in CI; it must pass on an unrestricted host before release.

The documented headless fixture command runs successfully: 9 total lines, 4 valid
DF17, 2 non-DF17, 3 malformed, 1 CPR solution, 0 drops. Both TUI fixture rendering
and q-to-quit were exercised in a real pseudo-terminal recording. No live SDR
capture exists. USB could not be initialized. **PENDING HARDWARE RUN**.

## Remaining limitations and release

Only Linux amd64 was built and smoke-tested. Live demodulator commands remain
unvalidated examples. Logs do not archive aircraft states; TCP diagnostics use
stderr. Input replay uses wall-clock arrival times, and overlong lines abort
replay. Navigation or flight-safety use is unsupported.

Preserve v1.0.0; propose v1.0.1 after successful unrestricted/remote CI, renewed
remote-ref inspection, and owner approval. Static Linux amd64 archive and SHA-256
checksums are prepared outside the source repository. Dependency notices are
included. No historical release is replaced. PROJECT.md remains local-only under
the repository's existing Git exclude rule.

## Verification environment and limits

Audit date: 2026-11-15. Pop!_OS 24.04, Linux amd64, AMD Ryzen 7 7700,
Go 1.27.3. Commands use `GOCACHE=/tmp/portfolio-go-cache` because the default
cache is read-only in this sandbox. No security restrictions were disabled.

Formatting, `go vet ./...`, `go build ./...`, static `CGO_ENABLED=0` binary
builds, and `go mod verify` passed. Workflow YAML parsed with PyYAML; job and
trigger structure was inspected. `actionlint` is unavailable. Minimum Go versions
and remote GitHub Actions have not been executed here. CI retains full tests.

GitHub CLI authentication is invalid. Shell DNS/network access to GitHub and the
module proxy is blocked. Public web profile review was possible, but repository
name availability, current remote refs/releases, vulnerability advisories, and
remote CI cannot be verified. No pushes, repository creation, tags, or release
publication were performed.

A heuristic scan of all working files (including ignored files) and reachable Git
history blobs found no credential-pattern matches. It covers private-key headers,
common GitHub/AWS/service token formats, and quoted credential assignments. This
is not a guarantee of absence. Dedicated gitleaks/govulncheck tools are unavailable.
The final local manifest and scan report are in the sibling publication review
folder. Git author identity uses the existing user configuration and includes its
configured email; it has not been changed or fabricated.
