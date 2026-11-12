package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Only release assets enforce the ancestor rule, and the message names the offending directory.
func TestCheckReleaseNodeDataDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	// Below the home directory: the default temporary directory is itself world-writable.
	base, err := os.MkdirTemp(home, "vd-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	if err := checkReleaseNodeDataDirectory(base, base); err != nil {
		t.Skipf("home ancestors are group/other writable: %v", err)
	}
	shared := filepath.Join(base, "shared")
	root := filepath.Join(shared, "data")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0775); err != nil {
		t.Fatal(err)
	}
	for profile, rejected := range map[string]bool{"release": true, "debug": false, "": false} {
		assets := filepath.Join(base, "assets-"+profile)
		if err := os.Mkdir(assets, 0700); err != nil {
			t.Fatal(err)
		}
		manifest := `{"version":1}`
		if profile != "" {
			manifest = `{"version":1,"profile":"` + profile + `"}`
		}
		if err := os.WriteFile(filepath.Join(assets, "assets.json"), []byte(manifest), 0600); err != nil {
			t.Fatal(err)
		}
		err := checkReleaseNodeDataDirectory(assets, root)
		if rejected != (err != nil) {
			t.Fatalf("profile %q: rejected=%v, err=%v", profile, rejected, err)
		}
		if rejected && !strings.Contains(err.Error(), shared+" is writable") {
			t.Fatalf("message does not name %s: %v", shared, err)
		}
		if err := checkReleaseNodeDataDirectory(assets, base); err != nil {
			t.Fatalf("profile %q rejected a private directory: %v", profile, err)
		}
	}
}
