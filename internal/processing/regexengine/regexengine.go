package regexengine

import "regexp"

// Pattern is a compiled regex, opaque to callers beyond MatchString
// and Extract.
type Pattern struct {
	re *regexp.Regexp
}

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
