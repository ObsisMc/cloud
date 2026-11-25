package execution

import (
	"errors"
	"strings"
)

// Host MCP servers are the HTTP MCP servers this server provides itself and has Node attach to
// every ACP setup of one session (AgentSessionSpec.host_mcp_servers; specs
// decisions/node/agent-runtime/20261118-host-mcp-servers-in-agent-session-spec.md). They are not
// plugin MCP: no selection, binding or lifecycle of MCPSelection applies to them. Their URLs and
// headers carry credentials, so they exist only in memory and in the one start command.

// HostMCPServer is one host MCP server as Node takes it.
type HostMCPServer struct {
	Name    string            `json:"name"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

// String keeps the URL and headers, which carry credentials, out of any formatted value.
func (s HostMCPServer) String() string { return "host MCP server " + s.Name }

// GoString is String, for %#v.
func (s HostMCPServer) GoString() string { return s.String() }

// HostServers is what a physical Agent is started with: its servers and the credentials in them,
// which the driver keeps out of the conversation it publishes.
type HostServers struct {
	Servers []HostMCPServer
	Secrets []string
}

// HostInstruction is a block only the Agent gets, after the turn's prompt: the turn records the
// prompt alone, and neither the block nor its token reaches the published conversation.
type HostInstruction struct {
	Text  string
	Token string
}

// HostTools gives one turn of a session its host tools. It is held in memory with the turn, never
// persisted, and one value serves one turn.
type HostTools interface {
	// Attach returns the servers the session's Agent is started with, issuing credentials for that
	// one physical Agent and withdrawing those of an earlier one. It is called only when the turn
	// starts the Agent; the servers then stay with it for its whole life.
	Attach(sessionID string) HostServers
	// Started says the start carrying the servers of the last Attach was sent to Node: only then is
	// there an execution whose Agent may connect to them.
	Started()
	// Instruct returns the turn's block, issued for one sending of the prompt: sending it again
	// withdraws the earlier token.
	Instruct() HostInstruction
}

// MaxHostInstruction bounds a host instruction, which shares Node's limit on a turn's text with
// the prompt and, after a recovery, the handoff.
const MaxHostInstruction = 512

// Failure codes of a session that could not get its host tools. Both end it before a prompt
// reached the Agent: the Node does not take host MCP servers, or its Agent does not speak HTTP MCP
// (Node's stable terminal detail).
const (
	FailureHostMCPNode  = "host_mcp_node_capability_missing"
	FailureHostMCPAgent = "host_mcp_http_capability_missing"
)

// HostMCPFailure reports whether code says a session could not get its host tools.
func HostMCPFailure(code string) bool {
	return code == FailureHostMCPNode || code == FailureHostMCPAgent
}

// ErrHostMCPUnavailable reports a Node that does not take host MCP servers.
var ErrHostMCPUnavailable = errors.New("the Node does not take host MCP servers")

// Limits of the wire grammar (D1 of the host MCP ADR); all are inclusive.
const (
	maxHostServers     = 4
	maxHostHeaders     = 8
	maxHostURLBytes    = 2 << 10
	maxHostTotalBytes  = 8 << 10
	maxHostHeaderName  = 64
	minHostServerName  = len(hostServerPrefix) + 1
	maxHostServerName  = 64
	hostServerPrefix   = "ora-"
	maxHostDNSName     = 253
	maxHostDNSLabel    = 63
	hostHeaderSymbols  = "!#$%&'*+-.^_`|~"
	hostSegmentSymbols = "-._~!$&'()*+,;=:@"
)

// reservedHostHeaders belong to the Agent's HTTP and MCP implementations, or are hop-by-hop.
var reservedHostHeaders = map[string]bool{
	"host": true, "content-length": true, "content-type": true, "accept": true, "mcp-session-id": true,
	"mcp-protocol-version": true, "last-event-id": true, "connection": true, "keep-alive": true,
	"proxy-connection": true, "te": true, "trailer": true, "transfer-encoding": true, "upgrade": true,
}

// ValidateHostMCPServers mirrors Node's validation character by character: Node closes the whole
// control connection on an invalid list, so nothing it would refuse may be sent. It matches the
// grammar directly instead of comparing a parsed URL, whose normalization differs between parsers.
// The error never contains a value.
func ValidateHostMCPServers(servers []HostMCPServer) error {
	if len(servers) > maxHostServers {
		return ErrInvalidRequest
	}
	total := 0
	for i, s := range servers {
		if !validHostServerName(s.Name) || i > 0 && servers[i-1].Name >= s.Name {
			return ErrInvalidRequest
		}
		if !validHostURL(s.URL) || s.Headers == nil || len(s.Headers) > maxHostHeaders {
			return ErrInvalidRequest
		}
		total += len(s.URL)
		for name, value := range s.Headers {
			if !validHostHeaderName(name) || !validHostHeaderValue(value) {
				return ErrInvalidRequest
			}
			total += len(name) + len(value)
		}
	}
	if total > maxHostTotalBytes {
		return ErrInvalidRequest
	}
	return nil
}

func validHostServerName(name string) bool {
	if len(name) < minHostServerName || len(name) > maxHostServerName || !strings.HasPrefix(name, hostServerPrefix) {
		return false
	}
	return all(name, func(c byte) bool { return lower(c) || digit(c) || c == '-' })
}

func validHostHeaderName(name string) bool {
	if name == "" || len(name) > maxHostHeaderName || reservedHostHeaders[name] {
		return false
	}
	return all(name, func(c byte) bool { return lower(c) || digit(c) || strings.IndexByte(hostHeaderSymbols, c) >= 0 })
}

func validHostHeaderValue(value string) bool {
	if value == "" || value[0] == ' ' || value[len(value)-1] == ' ' {
		return false
	}
	return all(value, func(c byte) bool { return c >= ' ' && c <= '~' })
}

// validHostURL matches the D1 grammar: scheme "://" host [":" port] path, with no query, fragment,
// userinfo or dot segment; plain http only to a literal loopback address.
func validHostURL(url string) bool {
	if len(url) > maxHostURLBytes {
		return false
	}
	rest, tls := strings.CutPrefix(url, "https://")
	if !tls {
		var plain bool
		if rest, plain = strings.CutPrefix(url, "http://"); !plain {
			return false
		}
	}
	slash := strings.IndexByte(rest, '/')
	if slash < 0 || !validHostPath(rest[slash:]) {
		return false
	}
	authority := rest[:slash]
	host, port, hasPort := authority, "", false
	if after, ok := strings.CutPrefix(authority, "[::1]"); ok {
		host = "[::1]"
		if after != "" {
			if port, hasPort = strings.CutPrefix(after, ":"); !hasPort {
				return false
			}
		}
	} else {
		host, port, hasPort = strings.Cut(authority, ":")
	}
	if hasPort && !validHostPort(port, tls) {
		return false
	}
	if host == "127.0.0.1" || host == "[::1]" {
		return true
	}
	return tls && (validIPv4(host) || validDNSName(host))
}

func validHostPort(port string, tls bool) bool {
	if port == "" || len(port) > 5 || port[0] == '0' || !all(port, digit) {
		return false
	}
	n := 0
	for i := range len(port) {
		n = n*10 + int(port[i]-'0')
	}
	if tls && n == 443 || !tls && n == 80 {
		return false // the scheme's default port is not canonical
	}
	return n <= 65535
}

func validIPv4(host string) bool {
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 3 || len(p) > 1 && p[0] == '0' || !all(p, digit) || len(p) == 3 && p > "255" {
			return false
		}
	}
	return true
}

func validDNSName(host string) bool {
	if len(host) > maxHostDNSName {
		return false
	}
	labels := strings.Split(host, ".")
	for _, l := range labels {
		if l == "" || len(l) > maxHostDNSLabel || l[0] == '-' || l[len(l)-1] == '-' || strings.HasPrefix(l, "xn--") ||
			!all(l, func(c byte) bool { return lower(c) || digit(c) || c == '-' }) {
			return false
		}
	}
	return lower(labels[len(labels)-1][0])
}

func validHostPath(path string) bool {
	for i, segment := range strings.Split(path, "/") {
		if i == 0 {
			continue // before the leading slash
		}
		if segment == "." || segment == ".." {
			return false
		}
		for j := 0; j < len(segment); j++ {
			c := segment[j]
			if c == '%' {
				if j+2 >= len(segment) || !upperHex(segment[j+1]) || !upperHex(segment[j+2]) || segment[j+1:j+3] == "2E" {
					return false
				}
				j += 2
				continue
			}
			if !lower(c) && !(c >= 'A' && c <= 'Z') && !digit(c) && strings.IndexByte(hostSegmentSymbols, c) < 0 {
				return false
			}
		}
	}
	return true
}

func lower(c byte) bool    { return c >= 'a' && c <= 'z' }
func digit(c byte) bool    { return c >= '0' && c <= '9' }
func upperHex(c byte) bool { return digit(c) || c >= 'A' && c <= 'F' }

func all(s string, ok func(byte) bool) bool {
	for i := range len(s) {
		if !ok(s[i]) {
			return false
		}
	}
	return true
}
