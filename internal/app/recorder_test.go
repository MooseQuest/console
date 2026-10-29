package app

import (
	"context"
	"testing"

	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/obs"
)

func TestDefaultRecorder(t *testing.T) {
	if DefaultRecorder(config.Config{}) != obs.Nop {
		t.Error("Metrics=false should yield obs.Nop")
	}
	if _, ok := DefaultRecorder(config.Config{Metrics: true}).(*obs.Memory); !ok {
		t.Error("Metrics=true should yield *obs.Memory")
	}
}

func TestWithRecorder(t *testing.T) {
	mem := obs.NewMemory()
	a, err := New(context.Background(), config.Config{DB: ""}, WithRecorder(func(config.Config) obs.Recorder { return mem }))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Obs != obs.Recorder(mem) {
		t.Error("custom Recorder factory not used")
	}
}
