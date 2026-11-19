package regexengine

import "regexp"

// Pattern is a compiled regex, opaque to callers beyond MatchString
// and Extract.
type Pattern struct {
	re                      *regexp.Regexp
	url, username, password int
}

func Compile(pattern string) (*Pattern, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	compiled := &Pattern{re: re, url: -1, username: -1, password: -1}
	for i, name := range re.SubexpNames() {
		switch name {
		case "url":
			compiled.url = i
		case "username":
			compiled.username = i
		case "password":
			compiled.password = i
		}
	}
	return compiled, nil
}

// MatchString reports whether line matches the pattern.
func (p *Pattern) MatchString(line string) bool {
	return p.re.MatchString(line)
}

func (p *Pattern) Extract(line string) (url, username, password string, ok bool) {
	match := p.re.FindStringSubmatch(line)
	if match == nil || p.url < 0 || p.username < 0 || p.password < 0 {
		return "", "", "", false
	}
	return match[p.url], match[p.username], match[p.password], true
}
