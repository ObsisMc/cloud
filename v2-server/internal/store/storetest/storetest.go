// Package storetest opens an isolated v2-server store for tests.
package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wanglongan587/cloud/v2-server/internal/store"
)

// Open gives the test its own PostgreSQL schema on TEST_DATABASE_URL, dropped afterwards. The
// test is skipped when TEST_DATABASE_URL is unset.
func Open(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, e := pgx.Connect(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { admin.Close(ctx) })
	var b [6]byte
	_, _ = rand.Read(b[:])
	schema := "v2test_" + hex.EncodeToString(b[:])
	if _, e := admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") })
	config, e := pgxpool.ParseConfig(url)
	if e != nil {
		t.Fatal(e)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	s, e := store.OpenConfig(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	return s
}

// SignIn ensures one demo-team account and returns its session token. Tests call it to sign in
// whoever they need without going through the browser's email login.
func SignIn(t *testing.T, s *store.Store, subject, displayName, role string) string {
	t.Helper()
	ctx := context.Background()
	email := Email(subject)
	if _, e := s.UpsertUser(ctx, store.PresetUser{Email: email, DisplayName: displayName, Role: role}); e != nil {
		t.Fatal(e)
	}
	u, e := s.UserByEmail(ctx, email)
	if e != nil {
		t.Fatal(e)
	}
	token, e := s.StartSession(ctx, u.ID, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	return token
}

// Email turns a test subject into the address its account uses; the address is the identity.
func Email(subject string) string {
	local := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		}
		return '-'
	}, strings.ToLower(subject))
	return local + "@demo.test"
}

// Login signs in a demo owner and returns their ID and the demo tenant's ID.
func Login(t *testing.T, s *store.Store, subject string) (user, tenant string) {
	t.Helper()
	ctx := context.Background()
	token := SignIn(t, s, subject, subject, store.RoleOwner)
	u, e := s.SessionUser(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	tenants, e := s.Tenants(ctx, u.ID)
	if e != nil || len(tenants) == 0 {
		t.Fatalf("tenants: %v %v", tenants, e)
	}
	return u.ID, tenants[0].ID
}
