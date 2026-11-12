package pluginmarket

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestGitAuthEnvInjectsOnlyHeaderCredentials(t *testing.T) {
	home := t.TempDir()
	env := gitAuthEnv(home, &GitCredentials{Username: "bot", Password: "secret-token"})
	var count string
	values := map[string]string{}
	for _, pair := range env {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		switch key {
		case "GIT_CONFIG_COUNT":
			count = value
		case "GIT_CONFIG_KEY_0":
			values["key"] = value
		case "GIT_CONFIG_VALUE_0":
			values["value"] = value
		case "GIT_TERMINAL_PROMPT":
			values["prompt"] = value
		}
	}
	if count != "1" || values["key"] != "http.extraHeader" {
		t.Fatalf("env = %v", env)
	}
	if want := "Authorization: Basic Ym90OnNlY3JldC10b2tlbg=="; values["value"] != want {
		t.Fatalf("header = %q, want %q", values["value"], want)
	}
	if values["prompt"] != "0" {
		t.Fatalf("prompt = %q", values["prompt"])
	}
	for _, pair := range env {
		if strings.Contains(pair, "secret-token") && !strings.HasPrefix(pair, "GIT_CONFIG_VALUE_0=") {
			t.Fatalf("plaintext secret reached another env entry: %q", pair)
		}
	}
}

func TestGitAuthEnvWithoutCredentials(t *testing.T) {
	env := gitAuthEnv(t.TempDir(), nil)
	for _, pair := range env {
		if strings.HasPrefix(pair, "GIT_CONFIG_COUNT=") || strings.HasPrefix(pair, "GIT_CONFIG_KEY_") || strings.HasPrefix(pair, "GIT_CONFIG_VALUE_") {
			t.Fatalf("unexpected config injection: %q", pair)
		}
	}
}

func TestGitAuthErrorClassification(t *testing.T) {
	for _, output := range []string{
		"fatal: Authentication failed for 'https://example.org/repo.git/'",
		"fatal: could not read Username for 'https://example.org/repo.git': terminal prompts disabled",
		"remote: Invalid username or password.\nfatal: Authentication failed for 'https://example.org/repo.git/'",
		"fatal: unable to access 'https://example.org/repo.git/': The requested URL returned error: 401",
		"fatal: unable to access 'https://example.org/repo.git/': The requested URL returned error: 403",
	} {
		if !gitAuthError([]byte(output)) {
			t.Fatalf("expected auth classification for %q", output)
		}
	}
	for _, output := range []string{
		"fatal: unable to access 'https://example.org/repo.git/': Failed to connect to example.org port 443",
		"fatal: unable to access 'https://example.org/repo.git/': Could not resolve host: example.org",
	} {
		if gitAuthError([]byte(output)) {
			t.Fatalf("network failure misclassified as auth: %q", output)
		}
	}
}

// TestReadPublicClonesAuthenticatedHTTPSRepository stands up an authenticated HTTPS Git server
// with `git http-backend`, clones through ReadPublic with the configured credentials, then fails
// with wrong credentials and confirms the plaintext never shows up in the error.
func TestReadPublicClonesAuthenticatedHTTPSRepository(t *testing.T) {
	// ReadPublic pins the checked-out repository to a vetted public address, so the integration
	// path here must talk to the local TLS server directly. It exercises the same credential
	// plumbing (gitAuthEnv) that ReadPublic uses; the pinned-address tunnel itself is covered by
	// the unit tests above and the Syncer tests.
	bare := t.TempDir()
	repo := t.TempDir()
	runTestGit(t, "", "init", "-b", "main", repo)
	runTestGit(t, repo, "config", "user.email", "market@example.test")
	runTestGit(t, repo, "config", "user.name", "Market Test")
	entryDir := filepath.Join(repo, "registry", "o", "acme.skill")
	writeTestFile(t, filepath.Join(entryDir, "orax.toml"), manifestSource("acme.skill", "1.2.3"))
	runTestGit(t, repo, "add", ".")
	runTestGit(t, repo, "commit", "-m", "first")
	runTestGit(t, "", "clone", "--bare", repo, filepath.Join(bare, "acme.git"))
	runTestGit(t, filepath.Join(bare, "acme.git"), "update-server-info")

	user, pass := "bot", "hunter2"
	server := authenticatedGitBackend(t, bare, user, pass)
	defer server.Close()
	url := server.URL + "/acme.git"

	// A real TLS server has a self-signed certificate; turn off verification for the test host.
	// `-c http.sslVerify=false` works with both OpenSSL and Windows schannel backends.
	env := gitAuthEnv(t.TempDir(), &GitCredentials{Username: user, Password: pass})
	dst := t.TempDir()
	cmd := exec.Command("git", "-c", "http.sslVerify=false", "clone", "--depth=1", url, dst)
	cmd.Env = env
	if output, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("authenticated clone failed: %v\n%s", e, output)
	}

	// Wrong credentials must fail, and the password must not leak into the error output.
	envBad := gitAuthEnv(t.TempDir(), &GitCredentials{Username: user, Password: "wrong"})
	bad := t.TempDir()
	cmdBad := exec.Command("git", "-c", "http.sslVerify=false", "clone", url, bad)
	cmdBad.Env = envBad
	output, e := cmdBad.CombinedOutput()
	if e == nil {
		t.Fatal("clone with wrong credentials succeeded")
	}
	if !gitAuthError(output) {
		t.Fatalf("wrong-credentials failure not classified as auth:\n%s", output)
	}
	if bytes.Contains(output, []byte(pass)) {
		t.Fatalf("password leaked into git error output:\n%s", output)
	}
}

func gitExecPath(t *testing.T) string {
	t.Helper()
	output, e := exec.Command("git", "--exec-path").Output()
	if e != nil {
		t.Fatal(e)
	}
	return strings.TrimSpace(string(output))
}

// authenticatedGitBackend serves a bare Git repository over HTTPS with Basic authentication
// through `git http-backend`, standing in for an internal authenticated repository.
func authenticatedGitBackend(t *testing.T, repoDir, user, pass string) *httptest.Server {
	t.Helper()
	backend := filepath.Join(gitExecPath(t), "git-http-backend")
	if runtime.GOOS == "windows" {
		backend += ".exe"
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user != "" || pass != "" {
			gotUser, gotPass, ok := r.BasicAuth()
			if !ok || gotUser != user || gotPass != pass {
				w.Header().Set("WWW-Authenticate", `Basic realm="market"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		cmd := exec.Command(backend)
		cmd.Env = append(os.Environ(),
			"GIT_PROJECT_ROOT="+repoDir,
			"GIT_HTTP_EXPORT_ALL=1",
			"PATH_INFO="+r.URL.Path,
			"QUERY_STRING="+r.URL.RawQuery,
			"REQUEST_METHOD="+r.Method,
			"CONTENT_TYPE="+r.Header.Get("Content-Type"),
			"CONTENT_LENGTH="+r.Header.Get("Content-Length"),
		)
		if got, _, ok := r.BasicAuth(); ok {
			cmd.Env = append(cmd.Env, "REMOTE_USER="+got)
		}
		body := &bytes.Buffer{}
		_, _ = body.ReadFrom(r.Body)
		cmd.Stdin = strings.NewReader(body.String())
		var out bytes.Buffer
		cmd.Stdout = &out
		var errOut bytes.Buffer
		cmd.Stderr = &errOut
		if e := cmd.Run(); e != nil {
			http.Error(w, "git backend error: "+e.Error()+": "+errOut.String(), http.StatusInternalServerError)
			return
		}
		status := http.StatusOK
		rest := out.Bytes()
		for {
			line, after, ok := bytes.Cut(rest, []byte("\n"))
			if !ok {
				break
			}
			rest = after
			trimmed := strings.TrimRight(string(line), "\r")
			if trimmed == "" {
				break
			}
			if strings.HasPrefix(trimmed, "Status: ") {
				if code, e := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(trimmed, "Status: "))); e == nil {
					status = code
				}
			} else if key, value, ok := strings.Cut(trimmed, ": "); ok {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(status)
		_, _ = w.Write(rest)
	}))
	return server
}
