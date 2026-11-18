package normalize

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// Normalized is the result of normalizing a raw URL string.
type Normalized struct {
	URL       string // normalized, canonical form
	Domain    string // empty for an IP-address host or an unregistrable host
	Subdomain string // empty if there is none
	Suffix    string // empty for an IP-address host
	Port      string // empty if none, or if it's the scheme's default port
}


func URL(raw string) (Normalized, error) {
	candidate := raw
	if !strings.Contains(candidate, "://") {
		candidate = "http://" + candidate
	}

	parsed, err := url.Parse(candidate)
	if err != nil {
		return Normalized{}, fmt.Errorf("normalize: parse %q: %w", raw, err)
	}
	if parsed.Host == "" {
		return Normalized{}, fmt.Errorf("normalize: %q has no host", raw)
	}

	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if isDefaultPort(scheme, port) {
		port = ""
	}

	hostport := host
	if port != "" {
		hostport = net.JoinHostPort(host, port)
	}

	path := parsed.Path
	if path == "/" {
		path = ""
	}

	normalizedURL := scheme + "://" + hostport + path
	if parsed.RawQuery != "" {
		normalizedURL += "?" + parsed.RawQuery
	}

	result := Normalized{URL: normalizedURL, Port: port}

	if ip := net.ParseIP(host); ip != nil {
		return result, nil // IP-address host: no domain/subdomain/suffix.
	}

	suffix, icann := publicsuffix.PublicSuffix(host)
	if !icann && suffix == host {
		// Unknown/private suffix with nothing registered under it -
		// leave domain/subdomain empty rather than guess.
		return result, nil
	}
	result.Suffix = suffix

	registrable, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		// No registrable domain under the public suffix list for this
		// host (e.g. the host is itself exactly the suffix).
		return result, nil
	}
	result.Domain = registrable

	if sub := strings.TrimSuffix(host, "."+registrable); sub != host && sub != "" {
		result.Subdomain = sub
	}

	return result, nil
}

func isDefaultPort(scheme, port string) bool {
	switch {
	case scheme == "http" && port == "80":
		return true
	case scheme == "https" && port == "443":
		return true
	default:
		return false
	}
}
