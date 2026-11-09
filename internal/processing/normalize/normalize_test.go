package normalize

import "testing"

func TestURL_SchemeLessDefaultsToHTTP(t *testing.T) {
	n, err := URL("example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.URL != "http://example.com" {
		t.Errorf("URL = %q, want %q", n.URL, "http://example.com")
	}
}

func TestURL_UppercaseHostLowercased(t *testing.T) {
	n, err := URL("http://EXAMPLE.COM")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.URL != "http://example.com" {
		t.Errorf("URL = %q, want lowercased host", n.URL)
	}
}

func TestURL_TrailingSlashStripped(t *testing.T) {
	withSlash, err := URL("http://example.com/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	withoutSlash, err := URL("http://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if withSlash.URL != withoutSlash.URL {
		t.Errorf("%q != %q, want trailing slash normalized away", withSlash.URL, withoutSlash.URL)
	}
}

func TestURL_ExplicitDefaultPortStripped(t *testing.T) {
	n, err := URL("http://example.com:80")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Port != "" {
		t.Errorf("Port = %q, want empty (default port stripped)", n.Port)
	}
	if n.URL != "http://example.com" {
		t.Errorf("URL = %q, want default port removed from normalized form", n.URL)
	}
}

func TestURL_ExplicitNonDefaultPortKept(t *testing.T) {
	n, err := URL("http://example.com:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Port != "8080" {
		t.Errorf("Port = %q, want %q", n.Port, "8080")
	}
	if n.URL != "http://example.com:8080" {
		t.Errorf("URL = %q, want non-default port kept", n.URL)
	}
}

func TestURL_MultiPartSuffix(t *testing.T) {
	n, err := URL("https://www.example.co.uk")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Domain != "example.co.uk" {
		t.Errorf("Domain = %q, want %q", n.Domain, "example.co.uk")
	}
	if n.Subdomain != "www" {
		t.Errorf("Subdomain = %q, want %q", n.Subdomain, "www")
	}
	if n.Suffix != "co.uk" {
		t.Errorf("Suffix = %q, want %q", n.Suffix, "co.uk")
	}
}

func TestURL_IPAddressHostHasNoDomainParts(t *testing.T) {
	n, err := URL("http://192.168.1.10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Domain != "" || n.Subdomain != "" || n.Suffix != "" {
		t.Errorf("got Domain=%q Subdomain=%q Suffix=%q, want all empty for an IP host",
			n.Domain, n.Subdomain, n.Suffix)
	}
}

func TestURL_UserinfoDroppedFromNormalizedForm(t *testing.T) {
	n, err := URL("http://user:pass@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.URL != "http://example.com" {
		t.Errorf("URL = %q, want userinfo stripped", n.URL)
	}
}

func TestURL_NoHostIsAnError(t *testing.T) {
	_, err := URL("http://")
	if err == nil {
		t.Fatal("expected an error for a URL with no host")
	}
}
