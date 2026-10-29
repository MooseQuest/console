package otel

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"

	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/obs"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestFactoryFallsBackWhenUnset(t *testing.T) {
	cfg := config.Default()
	f := NewFactory(env(nil), &bytes.Buffer{})
	if _, ok := f.Recorder(cfg).(*obs.Memory); !ok {
		t.Error("unset endpoint should yield the default *obs.Memory recorder")
	}
	cfg.Metrics = false
	f = NewFactory(env(map[string]string{EndpointEnv: "http://127.0.0.1:1"}), &bytes.Buffer{})
	if f.Recorder(cfg) != obs.Nop {
		t.Error("CONSOLE_METRICS=off should yield obs.Nop even with an endpoint")
	}
}

func TestFactoryBadEndpointWarnsAndFallsBack(t *testing.T) {
	var warn bytes.Buffer
	f := NewFactory(env(map[string]string{EndpointEnv: "not-a-url"}), &warn)
	if _, ok := f.Recorder(config.Default()).(*obs.Memory); !ok {
		t.Error("bad endpoint should fall back to the default recorder")
	}
	if warn.Len() == 0 {
		t.Error("expected a warning")
	}
}

func TestFactoryActivatesOTel(t *testing.T) {
	srv := httptest.NewServer(&fakeOTLP{})
	defer srv.Close()
	f := NewFactory(env(map[string]string{EndpointEnv: srv.URL}), &bytes.Buffer{})
	if _, ok := f.Recorder(config.Default()).(*Recorder); !ok {
		t.Fatal("endpoint set should yield the OTel recorder")
	}
	if err := f.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
