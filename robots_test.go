package main

import "testing"

func TestRobotsAllowed(t *testing.T) {
	aeon := readTestdata(t, "aeon_robots.txt")
	tokubai := readTestdata(t, "tokubai_robots.txt")
	for _, c := range []struct {
		robots []byte
		path   string
		want   bool
	}{
		{aeon, "/netsuper/sitemap.xml", true},
		{aeon, "/netsuper/01050000047260/", true},
		{aeon, "/netsuper/01050000047260/010500000472602000044588272.html", true},
		{aeon, "/netsuper/01050000047260/catalogsearch/result/", false},
		{aeon, "/netsuper/01050000047260/maintenance", false},
		{aeon, "/", false},
		// Tokubai blocks Anthropic's crawlers by name; our own token falls under "*".
		{tokubai, "/", true},
		{tokubai, "/2026/random_product", false},
	} {
		if got := robotsAllowed(c.robots, c.path); got != c.want {
			t.Errorf("%s: got %v, want %v", c.path, got, c.want)
		}
	}
	if robotsAllowed([]byte("User-agent: cabbage-coin\nDisallow: /\n\nUser-agent: *\nAllow: /\n"), "/x") {
		t.Error("a group naming our token must take precedence over *")
	}
}
