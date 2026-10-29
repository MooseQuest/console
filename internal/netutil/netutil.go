// Package netutil holds small network-address helpers shared by the CLI and
// the startup auth guard. It depends only on the standard library.
package netutil

import "net"

// IsLoopback reports whether addr binds only the loopback interface. An empty
// host (e.g. ":8080") or an unspecified one (e.g. "0.0.0.0:8080") binds all
// interfaces and is therefore not loopback. An unparseable addr is treated as
// non-loopback (fail safe).
func IsLoopback(addr string) bool {
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
