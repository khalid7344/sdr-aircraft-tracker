# Terminal demonstration

`replay.typescript` records the application running with bundled fixtures.
It contains ANSI terminal control sequences. Record a session after building:

```sh
script -q -c 'stty cols 140 rows 32; ./bin/sdr-tracker --mode=replay --replay-file=testdata/sample_frames.avr --replay-interval=50ms' /tmp/replay.typescript
# Press q to exit.
```

The README PNG renders text captured from a running terminal session on a dark
background using DejaVu Sans Mono. Only application output is included.
