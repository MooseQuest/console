package email

import (
	"context"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"github.com/moosequest/console/internal/core"
)

func sampleEvent() core.Event {
	return core.Event{
		Type:      core.EventComponentDown,
		Title:     "Component api is down",
		Message:   "errors 80/1000 (8.00%)",
		Component: "api",
		At:        time.Unix(1_700_000_000, 0).UTC(),
	}
}

func TestNotify_SendsMessage(t *testing.T) {
	var gotAddr, gotFrom string
	var gotTo []string
	var gotMsg []byte
	n := New(Config{
		Host:     "smtp.example.com",
		Port:     "587",
		Username: "user",
		Password: "pass",
		From:     "console@example.com",
		To:       []string{"ops@example.com", "oncall@example.com"},
	})
	n.send = func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
		gotAddr, gotFrom, gotTo, gotMsg = addr, from, to, msg
		return nil
	}

	if err := n.Notify(context.Background(), sampleEvent()); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if gotAddr != "smtp.example.com:587" {
		t.Errorf("addr = %q", gotAddr)
	}
	if gotFrom != "console@example.com" {
		t.Errorf("from = %q", gotFrom)
	}
	if len(gotTo) != 2 || gotTo[0] != "ops@example.com" || gotTo[1] != "oncall@example.com" {
		t.Errorf("to = %v", gotTo)
	}
	msg := string(gotMsg)
	if !strings.Contains(msg, "Subject: Component api is down") {
		t.Errorf("message missing subject:\n%s", msg)
	}
	if !strings.Contains(msg, "errors 80/1000 (8.00%)") {
		t.Errorf("message missing body:\n%s", msg)
	}
	if !strings.Contains(msg, "Component: api") {
		t.Errorf("message missing component:\n%s", msg)
	}
	if !strings.Contains(msg, "Severity: critical") {
		t.Errorf("message missing severity:\n%s", msg)
	}
}

func TestNotify_HeaderInjectionStripped(t *testing.T) {
	var gotMsg []byte
	n := New(Config{Host: "smtp.example.com", From: "console@example.com", To: []string{"ops@example.com"}})
	n.send = func(_ string, _ smtp.Auth, _ string, _ []string, msg []byte) error {
		gotMsg = msg
		return nil
	}
	ev := sampleEvent()
	ev.Title = "pwned\r\nBcc: attacker@evil.com\r\nX-Injected: 1"
	if err := n.Notify(context.Background(), ev); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	msg := string(gotMsg)
	// The title's CRLFs must be neutralized: no new header line is spawned.
	// (The sanitized text still appears on the single Subject line — that's fine.)
	if strings.Contains(msg, "\r\nBcc:") || strings.Contains(msg, "\r\nX-Injected:") {
		t.Fatalf("CRLF in title injected a header line:\n%q", msg)
	}
	// There must be exactly one Subject line, on its own header line.
	if strings.Count(msg, "Subject:") != 1 {
		t.Fatalf("expected exactly one Subject header:\n%q", msg)
	}
}

func TestNotify_MissingConfig(t *testing.T) {
	cases := map[string]Config{
		"no host": {From: "a@b.com", To: []string{"c@d.com"}},
		"no from": {Host: "smtp.example.com", To: []string{"c@d.com"}},
		"no to":   {Host: "smtp.example.com", From: "a@b.com"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			n := New(cfg)
			n.send = func(string, smtp.Auth, string, []string, []byte) error {
				t.Fatal("send should not be called with missing config")
				return nil
			}
			if err := n.Notify(context.Background(), sampleEvent()); err == nil {
				t.Fatal("expected error for missing config")
			}
		})
	}
}
