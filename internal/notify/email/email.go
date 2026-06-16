// Package email delivers Console events as plain-text email over SMTP. It uses
// only net/smtp from the standard library: PLAIN auth is used when a username is
// configured (so it should be paired with a TLS-capable submission port such as
// 587), and an unauthenticated send is attempted otherwise.
//
// The actual transmission is held behind an unexported send func field that
// defaults to smtp.SendMail. Tests substitute it to assert the recipients,
// sender, and message bytes without standing up a real SMTP server.
package email

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"strings"

	"github.com/moosequest/console/internal/core"
)

// Config carries the SMTP settings for a Notifier.
type Config struct {
	// Host is the SMTP server hostname.
	Host string
	// Port is the SMTP server port (e.g. "587").
	Port string
	// Username and Password enable PLAIN auth when Username is non-empty.
	Username string
	Password string
	// From is the envelope and header sender address.
	From string
	// To is the list of recipient addresses.
	To []string
}

// Notifier sends events as email over SMTP.
type Notifier struct {
	cfg Config
	// send performs the actual transmission. It defaults to smtp.SendMail and is
	// overridable in tests.
	send func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

// New builds an email notifier from cfg.
func New(cfg Config) *Notifier {
	return &Notifier{cfg: cfg, send: smtp.SendMail}
}

// Name identifies the sink.
func (n *Notifier) Name() string { return "email" }

// Notify builds an RFC 5322 message for ev and sends it via SMTP. The context
// is honored by returning early if it is already done before the send.
func (n *Notifier) Notify(ctx context.Context, ev core.Event) error {
	if n.cfg.Host == "" {
		return fmt.Errorf("email: no SMTP host configured")
	}
	if n.cfg.From == "" {
		return fmt.Errorf("email: no From address configured")
	}
	if len(n.cfg.To) == 0 {
		return fmt.Errorf("email: no To recipients configured")
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("email: %w", err)
	}

	port := n.cfg.Port
	if port == "" {
		port = "587"
	}
	addr := net.JoinHostPort(n.cfg.Host, port)

	var auth smtp.Auth
	if n.cfg.Username != "" {
		auth = smtp.PlainAuth("", n.cfg.Username, n.cfg.Password, n.cfg.Host)
	}

	msg := n.buildMessage(ev)

	send := n.send
	if send == nil {
		send = smtp.SendMail
	}
	if err := send(addr, auth, n.cfg.From, n.cfg.To, msg); err != nil {
		return fmt.Errorf("email: send: %w", err)
	}
	return nil
}

// buildMessage renders ev as an RFC 5322 message. Subject is the event title;
// the body summarizes the event's type, message, component, flag, and time.
func (n *Notifier) buildMessage(ev core.Event) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", headerSafe(n.cfg.From))
	fmt.Fprintf(&b, "To: %s\r\n", headerSafe(strings.Join(n.cfg.To, ", ")))
	fmt.Fprintf(&b, "Subject: %s\r\n", headerSafe(ev.Title))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("\r\n")

	fmt.Fprintf(&b, "Severity: %s\r\n", ev.Type.Severity())
	fmt.Fprintf(&b, "Type: %s\r\n", ev.Type)
	if ev.Message != "" {
		fmt.Fprintf(&b, "\r\n%s\r\n", ev.Message)
	}
	if ev.Component != "" {
		fmt.Fprintf(&b, "\r\nComponent: %s\r\n", ev.Component)
	}
	if ev.Flag != "" {
		fmt.Fprintf(&b, "Flag: %s\r\n", ev.Flag)
	}
	fmt.Fprintf(&b, "\r\nAt: %s\r\n", ev.At.UTC().Format("2006-01-02 15:04:05 MST"))
	return []byte(b.String())
}

// headerSafe strips CR/LF so a value (e.g. an event title) cannot inject
// additional SMTP headers or body content (header-injection guard).
func headerSafe(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}
