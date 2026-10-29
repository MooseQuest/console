package app

import (
	"context"
	"testing"

	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/obs"
)

func TestNewSetsObs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		metrics bool
	}{{"enabled", true}, {"disabled", false}} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.DB = ":memory:"
			cfg.Metrics = tc.metrics
			a, err := New(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			if a.Obs == nil {
				t.Fatal("App.Obs is nil")
			}
			if _, isMem := a.Obs.(*obs.Memory); isMem != tc.metrics {
				t.Fatalf("Memory=%v, want %v", isMem, tc.metrics)
			}
		})
	}
}
