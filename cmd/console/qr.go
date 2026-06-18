package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/mdp/qrterminal/v3"

	"github.com/moosequest/console/internal/config"
)

const qrUsage = `Show a QR code to open the Console dashboard on your phone.

Usage:
  console qr [-url <url>]

With no -url, it encodes http://<this machine's LAN IP>:<port> using the
configured listen port (CONSOLE_ADDR). Scan it from a phone on the SAME network.
Pass -url to encode any address instead (e.g. a tunnel URL for remote access).
`

func cmdQR(args []string, cfg config.Config) error {
	fs := flag.NewFlagSet("qr", flag.ContinueOnError)
	url := fs.String("url", "", "URL to encode (e.g. a tunnel URL); default derives the LAN URL")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *url != "" {
		return showQR(os.Stdout, *url, false, "")
	}
	ip, err := lanIP()
	if err != nil {
		return fmt.Errorf("could not detect this machine's LAN IP: %w (pass -url instead)", err)
	}
	port := addrPort(cfg.Addr)
	return showQR(os.Stdout, fmt.Sprintf("http://%s:%s", ip, port), isLoopback(cfg.Addr), port)
}

// showQR prints target plus a scannable terminal QR code. When derived (not a
// user-supplied URL), it adds same-network guidance and, if the server is bound
// to loopback, a note that it won't be reachable from the phone until bound to
// the LAN.
func showQR(w io.Writer, target string, loopback bool, port string) error {
	fmt.Fprintf(w, "\n  Scan to open Console:  %s\n\n", target)
	qrterminal.GenerateWithConfig(target, qrterminal.Config{
		Level:      qrterminal.M,
		Writer:     w,
		HalfBlocks: true,
		BlackChar:  qrterminal.BLACK_BLACK,
		WhiteChar:  qrterminal.WHITE_WHITE,
		QuietZone:  1,
	})
	if port == "" {
		return nil // user-supplied URL: no LAN guidance
	}
	fmt.Fprintln(w, "\n  Open it from a phone on the same Wi-Fi.")
	if loopback {
		fmt.Fprintf(w, "  NOTE: Console is bound to loopback; start it with CONSOLE_ADDR=:%s so the phone can reach it.\n", port)
		fmt.Fprintln(w, "  WARNING: binding off loopback exposes the unauthenticated dashboard to your network — use a trusted LAN.")
	}
	return nil
}

// lanIP returns this machine's primary non-loopback IPv4 address — the source
// address the OS would use for outbound traffic. The UDP "dial" sends no
// packets; it just resolves the route. Falls back to scanning interfaces.
func lanIP() (string, error) {
	if conn, err := net.Dial("udp", "8.8.8.8:80"); err == nil {
		defer conn.Close()
		if a, ok := conn.LocalAddr().(*net.UDPAddr); ok && a.IP.To4() != nil && !a.IP.IsLoopback() {
			return a.IP.String(), nil
		}
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			if ip := ipnet.IP.To4(); ip != nil && !ip.IsLoopback() {
				return ip.String(), nil
			}
		}
	}
	return "", fmt.Errorf("no non-loopback IPv4 address found")
}

// addrPort extracts the port from a listen address, defaulting to 8080.
func addrPort(addr string) string {
	if _, port, err := net.SplitHostPort(addr); err == nil && port != "" {
		return port
	}
	return "8080"
}

// isLoopback reports whether addr binds only the loopback interface (so a phone
// on the LAN can't reach it). An empty host (e.g. ":8080") binds all interfaces.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
