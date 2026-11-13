package server_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/pluginmarket"
	"github.com/wanglongan587/cloud/v2-server/internal/server"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
)

func workflowPluginArchive(t *testing.T) string {
	t.Helper()
	f, e := os.CreateTemp(t.TempDir(), "workflow-*.orax")
	if e != nil {
		t.Fatal(e)
	}
	z := zip.NewWriter(f)
	manifest, e := z.Create("orax.toml")
	if e != nil {
		t.Fatal(e)
	}
	_, _ = manifest.Write([]byte(`resolver = 1
title = "Requirement Development"
identifier = "ora-space.test-workflow"
namespace = "official"
kind = "workflow"
version = "0.1.0"
description = "A workflow."
`))
	document, e := z.Create("assets/workflows/requirement-development.json")
	if e != nil {
		t.Fatal(e)
	}
	_, _ = document.Write([]byte(`{"name":"需求开发","nodes":[{"id":"start","data":{"kind":"start"}},{"id":"output","data":{"kind":"output"}}],"edges":[{"source":"start","target":"output"}]}`))
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	if e := f.Close(); e != nil {
		t.Fatal(e)
	}
	return f.Name()
}

func seedTestPluginCatalog(t *testing.T, s *store.Store) {
	t.Helper()
	if e := s.ReplacePluginCatalog(context.Background(), []pluginmarket.Entry{
		{Namespace: "official", Identifier: "mattpocock.skills", Title: "Matt Pocock Skills", URL: "https://example.org/skill.orax", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Kind: "skill", Version: "1.3.1", Description: "Engineering skills", Readme: "# Matt Pocock Skills\n\nReal skills.", MarketplaceVisible: true, SourceURL: "https://github.com/ora-space/marketplace"},
		{Namespace: "official", Identifier: "ora-space.tavily-search", Title: "Tavily", Kind: "mcp", Version: "0.1.0", Description: "Web search", Readme: "# Tavily", MarketplaceVisible: true, SourceURL: "https://github.com/ora-space/marketplace"},
		{Namespace: "official", Identifier: "ora-space.workflow-examples", Title: "Workflow Examples", Kind: "workflow", Version: "0.1.0", Description: "Examples", Readme: "# Workflow Examples", MarketplaceVisible: true, SourceURL: "https://github.com/ora-space/marketplace"},
		{Namespace: "official", Identifier: "ora-space.opencode", Title: "OpenCode", Kind: "agent", Version: "0.6.8", Description: "Agent", Readme: "# OpenCode", MarketplaceVisible: true, SourceURL: "https://github.com/ora-space/marketplace"},
		{Namespace: "official", Identifier: "ora-space.codex", Title: "Codex", Kind: "agent", Version: "0.3.14", Description: "Agent", Readme: "# Codex", MarketplaceVisible: true, SourceURL: "https://github.com/ora-space/marketplace"},
		{Namespace: "official", Identifier: "test-pack", Title: "Test Pack", Kind: "pack", Version: "1.0.0", Description: "Pack", PackMembers: []string{"official/mattpocock.skills"}, MarketplaceVisible: true, SourceURL: "https://github.com/ora-space/marketplace"},
	}, time.Now()); e != nil {
		t.Fatal(e)
	}
}

func TestPluginMarketHTTP(t *testing.T) {
	f := newSquadServer(t)
	c := newContractClient(t, f.srv.URL)
	c.login()
	tenant := c.tenant()
	status, body := c.do("GET", "/api/v1/tenants/"+tenant+"/spaces", nil)
	if status != 200 {
		t.Fatalf("spaces = %d %v", status, body)
	}
	spaces, _ := body["items"].([]any)
	if len(spaces) != 1 {
		t.Fatalf("spaces = %v", body)
	}
	space := spaces[0].(map[string]any)["id"].(string)
	base := "/api/v1/tenants/" + tenant + "/spaces/" + space + "/plugins"

	status, body = c.do("GET", base+"/catalog", nil)
	if status != 200 {
		t.Fatalf("catalog = %d %v", status, body)
	}
	catalog, _ := body["items"].([]any)
	if len(catalog) != 6 || body["syncedAt"] == nil {
		t.Fatalf("catalog = %v", body)
	}
	agentIDs := map[string]bool{}
	for _, raw := range catalog {
		entry := raw.(map[string]any)
		if entry["kind"] == "agent" {
			agentIDs[entry["id"].(string)] = true
		}
	}
	if !agentIDs["official/ora-space.opencode"] || !agentIDs["official/ora-space.codex"] {
		t.Fatalf("catalog Agents = %v", agentIDs)
	}

	status, body = c.do("GET", base, nil)
	if status != 200 {
		t.Fatalf("plugins = %d %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("initial plugins = %v", body)
	}
	var preinstalledVersion float64
	for _, raw := range items {
		item := raw.(map[string]any)
		if item["sourceNamespace"] == "official" && item["identifier"] == "ora-space.opencode" {
			preinstalledVersion = item["version"].(float64)
		}
	}

	input := map[string]any{
		"identifier":    "official/mattpocock.skills",
		"pluginVersion": "1.3.1",
		"version":       0,
	}
	status, body = c.doHeaders("POST", base, input, map[string]string{
		"Idempotency-Key": "install-skill",
	})
	if status != 200 {
		t.Fatalf("install = %d %v", status, body)
	}
	resource := body["resource"].(map[string]any)
	if resource["kind"] != "skill" || resource["observedState"] != "installed" ||
		resource["observedVersion"] != "1.3.1" {
		t.Fatalf("installed = %v", resource)
	}
	version := resource["version"].(float64)

	status, replay := c.doHeaders("POST", base, input, map[string]string{
		"Idempotency-Key": "install-skill",
	})
	if status != 200 {
		t.Fatalf("replay = %d %v", status, replay)
	}
	if replayed := replay["resource"].(map[string]any); replayed["version"] != version {
		t.Fatalf("replayed = %v, want version %v", replayed, version)
	}
	status, body = c.doHeaders("POST", base, map[string]any{
		"identifier": "official/ora-space.tavily-search",
		"version":    0,
	}, map[string]string{"Idempotency-Key": "install-skill"})
	if status != 409 || body["code"] != "idempotency_conflict" {
		t.Fatalf("key reuse = %d %v", status, body)
	}

	status, body = c.doHeaders("POST", base, map[string]any{
		"identifier": "not-a-plugin",
		"version":    0,
	}, map[string]string{"Idempotency-Key": "invalid-plugin-id"})
	if status != 400 || body["code"] != "invalid_plugin_id" {
		t.Fatalf("invalid id = %d %v", status, body)
	}
	status, body = c.doHeaders("POST", base, map[string]any{
		"identifier":    "official/mattpocock.skills",
		"pluginVersion": "9.9.9",
		"version":       0,
	}, map[string]string{"Idempotency-Key": "missing-plugin-version"})
	if status != 400 || body["code"] != "plugin_version_unavailable" {
		t.Fatalf("unavailable version = %d %v", status, body)
	}
	status, body = c.doHeaders("POST", base, map[string]any{
		"identifier": "official/test-pack",
		"version":    0,
	}, map[string]string{"Idempotency-Key": "install-pack"})
	if status != 400 || body["code"] != "plugin_kind_not_installable" {
		t.Fatalf("pack = %d %v", status, body)
	}
	status, body = c.doHeaders("POST", base, map[string]any{
		"identifier":    "official/ora-space.codex",
		"pluginVersion": "0.3.14",
		"version":       0,
	}, map[string]string{"Idempotency-Key": "install-codex"})
	if status != 400 || body["code"] != "plugin_kind_not_installable" {
		t.Fatalf("install market agent = %d %v", status, body)
	}
	status, body = c.doHeaders("POST", base, map[string]any{
		"identifier": "official/ora-space.opencode",
		"version":    0,
	}, map[string]string{"Idempotency-Key": "install-runtime-agent"})
	if status != 409 || body["code"] != "plugin_runtime_managed" {
		t.Fatalf("preinstalled install = %d %v", status, body)
	}
	status, body = c.doHeaders("DELETE", base, map[string]any{
		"identifier": "official/ora-space.opencode",
		"version":    int64(preinstalledVersion),
	}, map[string]string{"Idempotency-Key": "remove-runtime-agent"})
	if status != 409 || body["code"] != "plugin_runtime_managed" {
		t.Fatalf("preinstalled removal = %d %v", status, body)
	}

	status, body = c.doHeaders("DELETE", base, map[string]any{
		"identifier": "official/mattpocock.skills",
	}, map[string]string{"Idempotency-Key": "remove-without-version"})
	if status != 428 || body["code"] != "version_required" {
		t.Fatalf("missing version = %d %v", status, body)
	}
	status, body = c.doHeaders("DELETE", base, map[string]any{
		"identifier": "official/mattpocock.skills",
		"version":    int64(version),
	}, map[string]string{"Idempotency-Key": "remove-skill"})
	if status != 200 {
		t.Fatalf("remove = %d %v", status, body)
	}
	removed := body["resource"].(map[string]any)
	if removed["desiredState"] != "removed" || removed["observedState"] != "removed" ||
		removed["observedVersion"] != nil {
		t.Fatalf("removed = %v", removed)
	}

	status, body = c.do("GET", base, nil)
	if status != 200 {
		t.Fatalf("plugins after remove = %d %v", status, body)
	}
	// The removed Skill row is gone; only the two preinstalled Agents remain.
	if items, _ := body["items"].([]any); len(items) != 2 {
		t.Fatalf("plugins after remove = %v", body)
	}
}

func TestHookPluginIsReadOnly(t *testing.T) {
	f := newSquadServer(t)
	c := newContractClient(t, f.srv.URL)
	c.login()
	tenant := c.tenant()
	status, body := c.do("GET", "/api/v1/tenants/"+tenant+"/spaces", nil)
	if status != 200 {
		t.Fatalf("spaces = %d %v", status, body)
	}
	spaces, _ := body["items"].([]any)
	if len(spaces) != 1 {
		t.Fatalf("spaces = %v", body)
	}
	space := spaces[0].(map[string]any)["id"].(string)
	base := "/api/v1/tenants/" + tenant + "/spaces/" + space + "/plugins"
	if e := f.store.ReplacePluginCatalog(context.Background(), []pluginmarket.Entry{{
		Namespace: "official", Identifier: "acme.hooks", Title: "Acme Hooks", Kind: "hook", Version: "1.0.0",
		Description: "Hooks", MarketplaceVisible: true, SourceURL: "https://example.org",
	}}, time.Now()); e != nil {
		t.Fatal(e)
	}

	status, body = c.do("GET", base+"/catalog", nil)
	if status != 200 {
		t.Fatalf("catalog = %d %v", status, body)
	}
	catalog, _ := body["items"].([]any)
	if len(catalog) != 1 {
		t.Fatalf("hook catalog = %v", body)
	}

	status, body = c.doHeaders("POST", base, map[string]any{
		"identifier":    "official/acme.hooks",
		"pluginVersion": "1.0.0",
		"version":       0,
	}, map[string]string{"Idempotency-Key": "install-hook"})
	if status != 400 || body["code"] != "plugin_kind_not_installable" {
		t.Fatalf("install hook = %d %v", status, body)
	}
}

func customWorkflowArchive(t *testing.T, identifier string) string {
	t.Helper()
	f, e := os.CreateTemp(t.TempDir(), "workflow-*.orax")
	if e != nil {
		t.Fatal(e)
	}
	z := zip.NewWriter(f)
	manifest, e := z.Create("orax.toml")
	if e != nil {
		t.Fatal(e)
	}
	_, _ = manifest.Write([]byte(`resolver = 1
title = "Acme Custom Workflow"
identifier = "` + identifier + `"
namespace = "acme"
kind = "workflow"
version = "0.1.0"
description = "A workflow from a custom source."
`))
	document, e := z.Create("assets/workflows/acme-custom.json")
	if e != nil {
		t.Fatal(e)
	}
	_, _ = document.Write([]byte(`{"name":"Acme Custom","nodes":[{"id":"start","data":{"kind":"start"}},{"id":"output","data":{"kind":"output"}}],"edges":[{"source":"start","target":"output"}]}`))
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	if e := f.Close(); e != nil {
		t.Fatal(e)
	}
	return f.Name()
}

// A custom (non-official) market source with sealed S3 credentials installs a workflow end to
// end: the package source resolves retrieval and credentials for the namespace, the download
// receives them, and verification plus import still complete.
func TestWorkflowInstallFromCustomCredentialedSource(t *testing.T) {
	t.Setenv("V2_MARKET_CREDENTIAL_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	archive := customWorkflowArchive(t, "acme.test-workflow")
	raw, e := os.ReadFile(archive)
	if e != nil {
		t.Fatal(e)
	}
	digest := sha256.Sum256(raw)
	digestHex := hex.EncodeToString(digest[:])

	var gotRetrieval pluginmarket.Retrieval
	var gotCredentials pluginmarket.Credentials
	f := newSquadServerWith(t, server.Options{
		SessionTTL:       time.Hour,
		ValidateWorkflow: func(string) error { return nil },
		DownloadPluginPackage: func(_ context.Context, _ string, _ string, r pluginmarket.Retrieval, c pluginmarket.Credentials) (string, error) {
			gotRetrieval, gotCredentials = r, c
			file, e := os.CreateTemp(t.TempDir(), "download-*.orax")
			if e != nil {
				return "", e
			}
			if _, e := file.Write(raw); e != nil {
				_ = file.Close()
				return "", e
			}
			if e := file.Close(); e != nil {
				return "", e
			}
			return file.Name(), nil
		},
	})
	c := newContractClient(t, f.srv.URL)
	c.login()
	tenant := c.tenant()
	status, body := c.do("GET", "/api/v1/tenants/"+tenant+"/spaces", nil)
	if status != 200 {
		t.Fatalf("spaces = %d %v", status, body)
	}
	spaces, _ := body["items"].([]any)
	if len(spaces) != 1 {
		t.Fatalf("spaces = %v", body)
	}
	space := spaces[0].(map[string]any)["id"].(string)
	base := "/api/v1/tenants/" + tenant + "/spaces/" + space + "/plugins"

	retrieval := pluginmarket.Retrieval{
		Mode: "s3_sigv4", Endpoint: "https://objects.example.org", Bucket: "custom-market", Region: "test",
	}
	creds := pluginmarket.Credentials{AccessKey: "AKIATESTCUSTOM", SecretKey: "custom-secret"}
	binding := strings.Join([]string{space, "acme", retrieval.Endpoint, retrieval.Bucket}, "/")
	sealed, e := pluginmarket.SealCredentials(creds, binding)
	if e != nil {
		t.Fatal(e)
	}
	config, e := json.Marshal(retrieval)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(f.ctx, `
		INSERT INTO v2_market_sources(space_id, namespace, url, branch, enabled, retrieval, credentials)
		VALUES ($1, 'acme', 'https://github.com/acme/custom-market.git', 'main', true, $2, $3)`,
		space, config, sealed); e != nil {
		t.Fatal(e)
	}
	if e := f.store.ReplacePluginCatalog(context.Background(), []pluginmarket.Entry{{
		Namespace: "acme", Identifier: "acme.test-workflow", Title: "Acme Custom",
		Kind: "workflow", Version: "0.1.0", Description: "A custom workflow",
		URL: "custom-workflow.orax", SHA256: digestHex,
		MarketplaceVisible: true, SourceURL: "https://github.com/acme/custom-market",
	}}, time.Now()); e != nil {
		t.Fatal(e)
	}

	status, body = c.doHeaders("POST", base, map[string]any{
		"identifier": "acme/acme.test-workflow", "pluginVersion": "0.1.0", "version": 0,
	}, map[string]string{"Idempotency-Key": "install-custom-workflow"})
	if status != 200 {
		t.Fatalf("custom workflow install = %d %v", status, body)
	}
	resource := body["resource"].(map[string]any)
	if resource["kind"] != "workflow" || resource["observedState"] != "installed" ||
		resource["observedVersion"] != "0.1.0" {
		t.Fatalf("installed custom workflow = %v", resource)
	}
	if gotRetrieval.Mode != "s3_sigv4" || gotCredentials.AccessKey != creds.AccessKey ||
		gotCredentials.SecretKey != creds.SecretKey {
		t.Fatalf("credentials not passed to download: %+v %+v", gotRetrieval, gotCredentials)
	}
	workflows, e := f.store.Workflows(context.Background(), tenant)
	if e != nil || len(workflows) != 1 {
		t.Fatalf("imported custom workflows = %+v %v", workflows, e)
	}
	if workflows[0].Name != "Acme Custom" {
		t.Fatalf("imported workflow name = %+v", workflows[0])
	}
}

func TestWorkflowPluginHTTPInstallAndRemoveKeepWorkflow(t *testing.T) {
	archive := workflowPluginArchive(t)
	f := newSquadServerWith(t, server.Options{
		SessionTTL:       time.Hour,
		ValidateWorkflow: func(string) error { return nil },
		DownloadPluginPackage: func(context.Context, string, string, pluginmarket.Retrieval, pluginmarket.Credentials) (string, error) {
			raw, e := os.ReadFile(archive)
			if e != nil {
				return "", e
			}
			file, e := os.CreateTemp(t.TempDir(), "download-*.orax")
			if e != nil {
				return "", e
			}
			if _, e := file.Write(raw); e != nil {
				_ = file.Close()
				return "", e
			}
			if e := file.Close(); e != nil {
				return "", e
			}
			return file.Name(), nil
		},
	})
	c := newContractClient(t, f.srv.URL)
	c.login()
	tenant := c.tenant()
	status, body := c.do("GET", "/api/v1/tenants/"+tenant+"/spaces", nil)
	if status != 200 {
		t.Fatalf("spaces = %d %v", status, body)
	}
	spaces, _ := body["items"].([]any)
	if len(spaces) != 1 {
		t.Fatalf("spaces = %v", body)
	}
	space := spaces[0].(map[string]any)["id"].(string)
	if e := f.store.ReplacePluginCatalog(context.Background(), []pluginmarket.Entry{{
		Namespace: "official", Identifier: "ora-space.test-workflow",
		Title: "Requirement Development", Kind: "workflow", Version: "0.1.0",
		Description: "A workflow", URL: "https://example.org/workflow.orax",
		SHA256:             "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		MarketplaceVisible: true, SourceURL: "https://github.com/ora-space/marketplace",
	}}, time.Now()); e != nil {
		t.Fatal(e)
	}
	original, _, e := f.store.CreateWorkflow(context.Background(), tenant, "需求开发", `{"nodes":[],"edges":[]}`)
	if e != nil {
		t.Fatal(e)
	}
	base := "/api/v1/tenants/" + tenant + "/spaces/" + space + "/plugins"
	input := map[string]any{
		"identifier":    "official/ora-space.test-workflow",
		"pluginVersion": "0.1.0",
		"version":       0,
	}
	status, body = c.doHeaders("POST", base, input, map[string]string{"Idempotency-Key": "install-workflow"})
	if status != 200 {
		t.Fatalf("install workflow = %d %v", status, body)
	}
	resource := body["resource"].(map[string]any)
	if resource["kind"] != "workflow" || resource["observedState"] != "installed" ||
		resource["observedVersion"] != "0.1.0" {
		t.Fatalf("installed workflow = %v", resource)
	}
	version := resource["version"].(float64)
	workflows, e := f.store.Workflows(context.Background(), tenant)
	if e != nil || len(workflows) != 2 {
		t.Fatalf("workflows = %+v %v", workflows, e)
	}
	names := map[string]bool{}
	for _, workflow := range workflows {
		names[workflow.Name] = true
	}
	if !names["需求开发"] || !names["需求开发（插件）"] {
		t.Fatalf("import did not preserve original and rename conflict: %+v", workflows)
	}
	if unchanged, e := f.store.Workflow(context.Background(), tenant, original.ID); e != nil || unchanged.Name != "需求开发" {
		t.Fatalf("original workflow changed = %+v %v", unchanged, e)
	}
	status, body = c.doHeaders("POST", base, input, map[string]string{"Idempotency-Key": "install-workflow"})
	if status != 200 || body["resource"].(map[string]any)["version"] != version {
		t.Fatalf("workflow replay = %d %v", status, body)
	}
	status, body = c.doHeaders("DELETE", base, map[string]any{
		"identifier": "official/ora-space.test-workflow",
		"version":    int64(version),
	}, map[string]string{"Idempotency-Key": "remove-workflow"})
	if status != 200 {
		t.Fatalf("remove workflow = %d %v", status, body)
	}
	if workflows, e := f.store.Workflows(context.Background(), tenant); e != nil || len(workflows) != 2 {
		t.Fatalf("remove deleted workflows: %+v %v", workflows, e)
	}
}
