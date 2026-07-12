package checklist

import (
	"strings"
	"testing"

	"cookie-hunter/internal/config"
	"cookie-hunter/internal/report"
)

// Shape matches real configuration.js output (tags.push entries).
var fixture = []byte(`
CookieConsent.configuration.tags.push({"type":"script","url":"https://www.youtube.com/iframe_api","cat":[4]});
CookieConsent.configuration.tags.push({"type":"script","url":"https://platform.twitter.com/widgets.js","cat":[5]});
CookieConsent.configuration.tags.push({"type":"script","url":"https://tvs.example/wp-content/themes/app/a11y.js","cat":[3]});
CookieConsent.configuration.tags.push({type:'iframe',url:'https://player.vimeo.com/video/1',cat:[4]});
`)

func cfg() *config.Site {
	c := config.Defaults()
	c.Domain = "tvs.example"
	c.CBID = "00000000-0000-0000-0000-000000000000"
	c.Region = "eu"
	return c
}

func TestParse(t *testing.T) {
	entries := Parse(fixture)
	if len(entries) != 4 {
		t.Fatalf("parsed %d entries, want 4", len(entries))
	}
	if entries[0].URL != "https://www.youtube.com/iframe_api" || len(entries[0].Cats) != 1 || entries[0].Cats[0] != 4 {
		t.Errorf("entry 0 = %+v", entries[0])
	}
	if entries[3].Type != "iframe" || entries[3].URL != "https://player.vimeo.com/video/1" {
		t.Errorf("unquoted-key entry = %+v", entries[3])
	}
}

func TestBlockedSemantics(t *testing.T) {
	for _, tc := range []struct {
		cats []int
		want bool
	}{
		{[]int{4}, true}, {[]int{2}, true}, {[]int{3}, true},
		{[]int{1}, false}, {[]int{5}, false}, {[]int{1, 5}, false}, {[]int{1, 4}, true}, {nil, false},
	} {
		if got := (Entry{Cats: tc.cats}).blocked(); got != tc.want {
			t.Errorf("blocked(%v) = %v, want %v", tc.cats, got, tc.want)
		}
	}
}

func TestRunChecks(t *testing.T) {
	checks := Run(fixture, 200, cfg(), nil)
	byID := map[string]report.Check{}
	for _, c := range checks {
		byID[c.ID] = c
	}

	if byID["C1"].Status != report.Pass {
		t.Errorf("C1 = %+v", byID["C1"])
	}
	// twitter widgets.js at cat [5] = tracker not blocked (the real tvs.pl finding)
	if byID["C2"].Status != report.Fail || !strings.Contains(byID["C2"].Detail, "platform.twitter.com") {
		t.Errorf("C2 = %+v, want fail mentioning twitter", byID["C2"])
	}
	// first-party a11y.js at cat [3] would break when auto-block activates
	if byID["C3"].Status != report.Fail || !strings.Contains(byID["C3"].Detail, "a11y.js") {
		t.Errorf("C3 = %+v, want fail mentioning a11y.js", byID["C3"])
	}
	if byID["C4"].Status != report.Skip {
		t.Errorf("C4 without previous snapshot = %+v, want skip", byID["C4"])
	}
}

func TestRun404(t *testing.T) {
	checks := Run(nil, 404, cfg(), nil)
	if checks[0].ID != "C1" || checks[0].Status != report.Fail {
		t.Errorf("C1 on 404 = %+v, want fail", checks[0])
	}
}

func TestDrift(t *testing.T) {
	prev := fixture[:len(fixture)-100]
	checks := Run(fixture, 200, cfg(), prev)
	last := checks[len(checks)-1]
	if last.ID != "C4" || last.Status != report.Pass || !strings.Contains(last.Detail, "CHANGED") {
		t.Errorf("C4 with drift = %+v", last)
	}
}
