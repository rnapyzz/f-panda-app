package httpx

import (
	"net"
	"net/http"
)

// ClientIP extracts the caller's IP from RemoteAddr, stripping the port.
// Falls back to the raw RemoteAddr when it isn't in host:port form (e.g. in
// tests using httptest, which sets an unparseable placeholder).
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
