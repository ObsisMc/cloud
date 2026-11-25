//go:build linux

// Package listentest tells tests where their own process listens.
package listentest

import (
	"encoding/binary"
	"encoding/hex"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Listening lists the local addresses of this process's listening TCP sockets.
func Listening(t testing.TB) []netip.AddrPort {
	t.Helper()
	descriptors, err := filepath.Glob("/proc/self/fd/*")
	if err != nil {
		t.Fatal(err)
	}
	own := map[string]bool{}
	for _, d := range descriptors {
		if target, err := os.Readlink(d); err == nil { // one may be gone by now
			if inode, ok := strings.CutPrefix(target, "socket:["); ok {
				own[strings.TrimSuffix(inode, "]")] = true
			}
		}
	}
	var addresses []netip.AddrPort
	for _, table := range []string{"/proc/self/net/tcp", "/proc/self/net/tcp6"} {
		data, err := os.ReadFile(table)
		if os.IsNotExist(err) { // a host without IPv6
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n")[1:] {
			// sl local_address rem_address st tx:rx tr:when retrnsmt uid timeout inode
			if fields := strings.Fields(line); len(fields) > 9 && fields[3] == "0A" && own[fields[9]] {
				addresses = append(addresses, local(t, fields[1]))
			}
		}
	}
	return addresses
}

// local reads the kernel's form of an address, whose 32-bit words are in host order.
func local(t testing.TB, text string) netip.AddrPort {
	t.Helper()
	host, port, _ := strings.Cut(text, ":")
	raw, err := hex.DecodeString(host)
	if err != nil || len(raw) != 4 && len(raw) != 16 {
		t.Fatalf("address %q: %v", text, err)
	}
	for i := 0; i < len(raw); i += 4 {
		binary.NativeEndian.PutUint32(raw[i:], binary.BigEndian.Uint32(raw[i:]))
	}
	number, err := strconv.ParseUint(port, 16, 16)
	if err != nil {
		t.Fatalf("address %q: %v", text, err)
	}
	address, _ := netip.AddrFromSlice(raw)
	return netip.AddrPortFrom(address, uint16(number))
}
