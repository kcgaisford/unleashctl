package client

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveGetEnvironments is an opt-in integration check against a real,
// running Unleash instance (spec §8: integration tests run against a
// disposable Unleash Docker instance). Skipped unless both env vars are set,
// so `go test ./...` stays hermetic by default.
func TestLiveGetEnvironments(t *testing.T) {
	url := os.Getenv("UNLEASHCTL_LIVE_URL")
	token := os.Getenv("UNLEASHCTL_LIVE_TOKEN")
	if url == "" || token == "" {
		t.Skip("set UNLEASHCTL_LIVE_URL and UNLEASHCTL_LIVE_TOKEN to run against a live instance")
	}

	c := New(url, token)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	envs, err := c.GetEnvironments(ctx)
	if err != nil {
		t.Fatalf("GetEnvironments: %v", err)
	}
	if len(envs.Environments) == 0 {
		t.Fatal("expected at least one environment, got none")
	}
	t.Logf("found %d environment(s)", len(envs.Environments))
}
