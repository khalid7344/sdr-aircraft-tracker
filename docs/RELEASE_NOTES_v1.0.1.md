# v1.0.1

[Published release](https://github.com/khalid7344/sdr-aircraft-tracker/releases/tag/v1.0.1).
Source: `95f100c666ad585e06b52d09230813aef50bb7a7`. The v1.0.0 tag is preserved.

- Correct Gillham altitude bit mapping and valid Gray-code handling.
- Accept the valid Q-bit altitude of -1000 feet; fix polar CPR boundary handling.
- Reject malformed short hex frames and out-of-range encoded CPR inputs.
- Return replay source and TUI failures to the shell; drain pipeline before shutdown.
- Validate timing arguments; restrict newly created logs to owner access.
- Repair clone/license links and clarify logging, benchmarks, and validation status.
- Add --version with release version/source commit and bundle the replay fixture.
- Add CLI and protocol regression tests; CI checks minimum and stable Go.

Validation: deterministic reference-frame replay and unit/race tests. The publication
receipt records [successful release-commit CI](https://github.com/khalid7344/sdr-aircraft-tracker/actions/runs/43795274888).
Live RTL-SDR hardware validation has not occurred.

Assets: static Linux amd64 archive and SHA256SUMS. No ARM64, macOS or Windows binary
is provided. [Download and verification instructions](../README.md#linux-amd64-release).

Archive SHA-256:

```text
f84c7d01768739e1c16378fcfe952c1d69525a80a9e071a3859210b4ddea8908
```

Documentation changes on main do not change the published archive or tag.
