package config

import "testing"

func TestPublicAddressRejectsUnsafeOrAmbiguousURLs(t *testing.T) {
	for _, s := range []string{"", "ftp://host", "http://user:pass@host", "http://host/path", "http://host?x=1", "http://host#x", "http://host;evil", "http://host/$(evil)", "http://host\n"} {
		if _, err := PublicAddress(s); err == nil {
			t.Fatalf("接受了 %q", s)
		}
	}
	for _, s := range []string{"http://panel:7226", "https://panel.example/", "http://[::1]:7226"} {
		if _, err := PublicAddress(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}
