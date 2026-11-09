// Package regexengine is the single seam where Reader's processing
// pipeline depends on a concrete regex implementation. Today it wraps
// the standard library's regexp (RE2). If the real predefined Regex
// patterns ever need backreferences or lookahead (see
// specs/tech-stack.md, "Regex Engine - Important Open Risk"), only
// this package needs to change - regexselect and extract call into it
// rather than importing "regexp" directly, so they don't need
// rewriting when that day comes.
package regexengine

import "regexp"

// Pattern is a compiled regex, opaque to callers beyond MatchString
// and Extract.
type Pattern struct {
	re *regexp.Regexp
}

// Compile compiles a pattern string. It returns an error for any
// pattern the current engine cannot compile; per
// specs/2026-11-09-core-file-processing-pipeline/plan.md Group 2,
// callers are expected to skip such patterns (logged, not fatal)
// rather than treat this as fatal to the whole file.
func Compile(pattern string) (*Pattern, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &Pattern{re: re}, nil
}

// MatchString reports whether line matches the pattern.
func (p *Pattern) MatchString(line string) bool {
	return p.re.MatchString(line)
}

// Extract returns the named capture groups "url", "username", and
// "password" for the first match in line. ok is false if line does
// not match at all, or if the pattern itself is missing one of the
// three required group names (a pattern-authoring problem, not a
// per-line one).
func (p *Pattern) Extract(line string) (url, username, password string, ok bool) {
	match := p.re.FindStringSubmatch(line)
	if match == nil {
		return "", "", "", false
	}

	names := p.re.SubexpNames()
	values := make(map[string]string, len(names))
	for i, name := range names {
		if name == "" {
			continue
		}
		values[name] = match[i]
	}

	url, hasURL := values["url"]
	username, hasUsername := values["username"]
	password, hasPassword := values["password"]
	if !hasURL || !hasUsername || !hasPassword {
		return "", "", "", false
	}
	return url, username, password, true
}
