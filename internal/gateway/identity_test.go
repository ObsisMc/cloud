package gateway

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeMatchesCloudByteLimits(t *testing.T) {
	longASCII := strings.Repeat("n", 255)
	// 100 three-byte runes are 300 bytes: Cloud counts bytes, so the name must shrink to 66 whole runes.
	cjk := strings.Repeat("名", 100)
	cases := []struct {
		name    string
		in      VerifiedIdentity
		want    VerifiedIdentity
		wantErr bool
	}{
		{"unchanged when within limits", VerifiedIdentity{"github.com", "42", "Ray"}, VerifiedIdentity{"github.com", "42", "Ray"}, false},
		{"long ascii name truncated to 200 bytes", VerifiedIdentity{"github.com", "42", longASCII}, VerifiedIdentity{"github.com", "42", longASCII[:200]}, false},
		{"multibyte name truncated on a rune boundary", VerifiedIdentity{"github.com", "42", cjk}, VerifiedIdentity{"github.com", "42", strings.Repeat("名", 66)}, false},
		{"invalid utf8 name dropped", VerifiedIdentity{"github.com", "42", "bad\xff"}, VerifiedIdentity{"github.com", "42", ""}, false},
		{"empty source rejected", VerifiedIdentity{"", "42", ""}, VerifiedIdentity{}, true},
		{"empty subject rejected", VerifiedIdentity{"github.com", "", ""}, VerifiedIdentity{}, true},
		{"source over 128 bytes rejected", VerifiedIdentity{strings.Repeat("s", 129), "42", ""}, VerifiedIdentity{}, true},
		{"subject over 512 bytes rejected", VerifiedIdentity{"github.com", strings.Repeat("s", 513), ""}, VerifiedIdentity{}, true},
	}
	for _, tc := range cases {
		got, e := Normalize(tc.in)
		if (e != nil) != tc.wantErr || got != tc.want {
			t.Errorf("%s: Normalize = (%+v,%v) want (%+v, err=%v)", tc.name, got, e, tc.want, tc.wantErr)
		}
		if e != nil && !errors.Is(e, ErrProviderRejected) {
			t.Errorf("%s: rejection must map to ErrProviderRejected, got %v", tc.name, e)
		}
		if len(got.DisplayName) > MaxDisplayNameLength || !utf8.ValidString(got.DisplayName) {
			t.Errorf("%s: display name %q violates the byte limit or UTF-8 validity", tc.name, got.DisplayName)
		}
	}
}
