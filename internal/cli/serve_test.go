package cli

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/moosequest/console/internal/config"
)

func authCfg(addr, metrics, mode string, admin ...string) config.Config {
	c := config.Default()
	c.Addr, c.MetricsAddr, c.AuthMode = addr, metrics, mode
	c.Tokens.Admin = admin
	return c
}

func TestCheckExposure(t *testing.T) {
	cases := []struct {
		name    string
		cfg     config.Config
		wantErr bool
		warn    bool
	}{
		{"loopback open", authCfg("127.0.0.1:8080", "", "auto"), false, false},
		{"localhost open", authCfg("localhost:8080", "", "auto"), false, false},
		{"all-interfaces no auth", authCfg(":8080", "", "auto"), true, false},
		{"0.0.0.0 no auth", authCfg("0.0.0.0:8080", "", "auto"), true, false},
		{"non-loopback with token", authCfg(":8080", "", "auto", "tok"), false, false},
		{"non-loopback disabled warns", authCfg(":8080", "", "disabled"), false, true},
		{"disabled overrides tokens, warns", authCfg(":8080", "", "disabled", "tok"), false, true},
		{"loopback disabled quiet", authCfg("127.0.0.1:8080", "", "disabled"), false, false},
		{"metrics non-loopback no auth", authCfg("127.0.0.1:8080", ":9090", "auto"), true, false},
		{"metrics non-loopback with token", authCfg("127.0.0.1:8080", ":9090", "auto", "tok"), false, false},
		{"metrics loopback ok", authCfg("127.0.0.1:8080", "127.0.0.1:9090", "auto"), false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			old := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
			defer slog.SetDefault(old)

			err := checkExposure(tc.cfg)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				for _, want := range []string{"CONSOLE_ADMIN_TOKENS", "CONSOLE_AUTH=disabled"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q must name %q", err, want)
					}
				}
			}
			if got := strings.Contains(buf.String(), "level=WARN"); got != tc.warn {
				t.Errorf("warn = %v, want %v (log %q)", got, tc.warn, buf.String())
			}
		})
	}
}
