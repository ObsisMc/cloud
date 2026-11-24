package execution

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// testdata/host_mcp_servers.json is desktop's crates/node-protocol/fixtures/host_mcp_servers.json
// at the pinned Node commit (scripts/prepare-node.sh). Node and Controller must reach the labelled
// verdict for every case, so refresh it with the pin.
func TestHostMCPServersMatchNodesSharedSamples(t *testing.T) {
	data, err := os.ReadFile("testdata/host_mcp_servers.json")
	if err != nil {
		t.Fatal(err)
	}
	var samples struct {
		URLCases []struct {
			URL, Note string
			Accept    bool
		} `json:"url_cases"`
		ListCases []struct {
			Note    string
			Accept  bool
			Servers json.RawMessage `json:"host_mcp_servers"`
		} `json:"list_cases"`
	}
	if err := json.Unmarshal(data, &samples); err != nil {
		t.Fatal(err)
	}
	if len(samples.URLCases) == 0 || len(samples.ListCases) == 0 {
		t.Fatal("no samples")
	}
	for _, c := range samples.URLCases {
		err := ValidateHostMCPServers([]HostMCPServer{{Name: "ora-test", URL: c.URL, Headers: map[string]string{}}})
		if (err == nil) != c.Accept {
			t.Errorf("%s (%q): accepted %v, want %v", c.Note, c.URL, err == nil, c.Accept)
		}
	}
	// Cases about the JSON shape alone (an unknown field, a value of another type) and raw_cases
	// cannot be expressed by HostMCPServer, so this Controller cannot send them: a list that does
	// not decode into it counts as refused.
	for _, c := range samples.ListCases {
		var servers []HostMCPServer
		dec := json.NewDecoder(bytes.NewReader(c.Servers))
		dec.DisallowUnknownFields()
		err := dec.Decode(&servers)
		if err == nil {
			err = ValidateHostMCPServers(servers)
		}
		if (err == nil) != c.Accept {
			t.Errorf("%s: accepted %v, want %v", c.Note, err == nil, c.Accept)
		}
	}
}

func TestAHostMCPServerNeverFormatsItsCredentials(t *testing.T) {
	s := HostMCPServer{Name: "ora-x", URL: "http://127.0.0.1:1/mcp/secret", Headers: map[string]string{"authorization": "Bearer secret"}}
	for _, text := range []string{fmt.Sprint(s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s), fmt.Sprintf("%v", []HostMCPServer{s})} {
		if strings.Contains(text, "secret") {
			t.Fatalf("formatted %q", text)
		}
	}
}
