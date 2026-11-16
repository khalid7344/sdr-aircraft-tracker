package main

import (
	"testing"
)

func TestRunRejectsBadInputAndMissingReplay(t *testing.T) {
	for _, args := range [][]string{
		{"--mode=unknown"}, {"--mode=replay", "--ttl=0"},
		{"--mode=replay", "--replay-interval=-1ms"},
		{"--mode=replay", "--headless", "--replay-file=" + t.TempDir() + "/missing.avr"},
	} {
		if err := run(args); err == nil {
			t.Errorf("expected error for %v", args)
		}
	}
}

func TestRunFixtureReplay(t *testing.T) {
	if err := run([]string{"--mode=replay", "--headless", "--replay-interval=0", "--replay-file=../../testdata/sample_frames.avr"}); err != nil {
		t.Fatal(err)
	}
}

func TestRunVersionDoesNotStartIngestion(t *testing.T) {
	if err := run([]string{"--version"}); err != nil {
		t.Fatal(err)
	}
}
