package main

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"
)

// robotsToken is how robots.txt groups can address this crawler.
const robotsToken = "cabbage-coin"

// robotsAllowed reports whether path may be fetched under robots.txt, following
// RFC 9309: use the group naming our token, else the "*" group; the longest
// matching rule wins and allow wins ties.
func robotsAllowed(robots []byte, path string) bool {
	type rule struct {
		allow   bool
		pattern string
	}
	var mine, star []rule
	var agents []string
	inRules := false
	sc := bufio.NewScanner(bytes.NewReader(robots))
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		switch key {
		case "user-agent":
			if inRules { // a user-agent after rules starts a new group
				agents, inRules = nil, false
			}
			agents = append(agents, strings.ToLower(val))
		case "allow", "disallow":
			inRules = true
			if val == "" {
				continue // "Disallow:" with no path allows everything
			}
			r := rule{allow: key == "allow", pattern: val}
			for _, a := range agents {
				switch {
				case a == "*":
					star = append(star, r)
				case strings.Contains(robotsToken, a):
					mine = append(mine, r)
				}
			}
		}
	}
	rules := star
	if mine != nil {
		rules = mine
	}
	best, allowed := -1, true
	for _, r := range rules {
		if !robotsMatch(r.pattern, path) {
			continue
		}
		if len(r.pattern) > best || (len(r.pattern) == best && r.allow) {
			best, allowed = len(r.pattern), r.allow
		}
	}
	return allowed
}

func robotsMatch(pattern, path string) bool {
	expr := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*")
	if strings.HasSuffix(expr, `\$`) {
		expr = strings.TrimSuffix(expr, `\$`) + "$"
	}
	ok, _ := regexp.MatchString(expr, path)
	return ok
}
