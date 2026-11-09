// Package normalize turns a raw extracted URL into a canonical form
// and derives its domain/subdomain/suffix/port. See
// specs/2026-11-09-core-file-processing-pipeline/plan.md Group 5.
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

// URL normalizes raw into a canonical form and extracts its
// domain/subdomain/suffix/port. Documented edge-case behavior (see
// plan.md Group 5):
//
//   - A scheme-less input (no "://") is assumed to be "http://".
//   - Scheme and host are lowercased.
//   - A bare-path trailing slash ("http://example.com/") is removed,
//     so "http://example.com" and "http://example.com/" normalize
//     identically.
//   - An explicit default port for the scheme (":80" for http, ":443"
//     for https) is stripped as redundant.
//   - Any userinfo in the input (e.g. "user:pass@host") is dropped -
//     it never appears in the normalized form.
//   - An IP-address host has no domain/subdomain/suffix; those fields
//     come back empty, not an error.
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
