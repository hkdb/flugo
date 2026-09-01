//go:build android

package bridge

import (
	"os"
	"strings"
)

// Android has no /etc/resolv.conf, so Go's pure resolver can fall back to
// 127.0.0.1:53 and fail every lookup with "no such host". Force the cgo
// resolver (bionic getaddrinfo) unless the embedder already chose one.
// init() runs before any lookup, which is when net reads GODEBUG.
func init() {
	godebug := os.Getenv("GODEBUG")
	if strings.Contains(godebug, "netdns=") {
		return
	}
	if godebug != "" {
		godebug += ","
	}
	_ = os.Setenv("GODEBUG", godebug+"netdns=cgo")
}
