# v1.0.1 — proposed patch release

Not published. Preserve v1.0.0 and existing history.

- Correct Gillham altitude bit mapping and valid Gray-code handling.
- Accept the valid Q-bit altitude of -1000 feet; fix polar CPR boundary handling.
- Reject malformed short hex frames and out-of-range encoded CPR inputs.
- Return replay source and TUI failures to the shell; drain pipeline before shutdown.
- Validate timing arguments; restrict newly created logs to owner access.
- Repair clone/license links and clarify logging, benchmarks, and validation status.
- Add --version with release version/source commit and bundle the replay fixture.
- Add CLI and protocol regression tests; CI checks minimum and stable Go.

Validation: see README and the local publication review. Full local race run is
blocked solely at the loopback TCP listener by sandbox policy. Hardware validation
is pending. Remote CI must pass before publishing a release.

Proposed asset: static Linux amd64 binary archive, plus SHA256SUMS. No ARM64,
macOS, or Windows validation or binaries are claimed.
