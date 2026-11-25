package completiontool

import (
	"net/netip"
	"net/url"
	"testing"

	"github.com/wanglongan587/cloud/v2-server/internal/listentest"
)

// The endpoint is reachable on this host alone, and Node is given that address in clear text: the
// Agent shares this network namespace (consumer ADR D1).
func TestTheEndpointListensOnLoopbackOnly(t *testing.T) {
	before := map[netip.AddrPort]bool{}
	for _, at := range listentest.Listening(t) {
		before[at] = true
	}
	h := serve(t)
	var opened []netip.AddrPort
	for _, at := range listentest.Listening(t) {
		if !before[at] {
			opened = append(opened, at)
		}
	}
	if len(opened) != 1 || opened[0].Addr() != netip.MustParseAddr("127.0.0.1") {
		t.Fatalf("the endpoint listens on %v", opened)
	}
	u, err := url.Parse(attach(t, h.r.Attempt("n1", "n1"), "s1"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "http" || u.Host != opened[0].String() || u.User != nil || u.RawQuery != "" {
		t.Fatalf("Node is given %s://%s", u.Scheme, u.Host)
	}
}
