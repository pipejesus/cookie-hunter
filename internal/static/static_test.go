package static

import (
	"os"
	"strings"
	"testing"

	"cookie-hunter/internal/config"
	"cookie-hunter/internal/report"
)

func cfg() *config.Site {
	c := config.Defaults()
	c.Domain = "tvs.example"
	c.Region = "eu"
	return c
}

func run(t *testing.T, fixture string, c *config.Site) map[string]report.Check {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/" + fixture)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]report.Check{}
	for _, ch := range Run(raw, "https://"+c.Domain+"/", c) {
		out[ch.ID] = ch
	}
	return out
}

// gated.html is a fully compliant page — every check must pass. It contains the
// P1 trap (data-cookieblock-src holding a youtube URL): a naive ` src=` grep
// would fail S3 here.
func TestGatedPageAllPass(t *testing.T) {
	for id, ch := range run(t, "gated.html", cfg()) {
		if ch.Status != report.Pass {
			t.Errorf("%s = %s (%s), want pass", id, ch.Status, ch.Detail)
		}
	}
}

func TestLivePageFailures(t *testing.T) {
	c := cfg()
	checks := run(t, "live.html", c)

	wantFail := map[string]string{
		"S1": "async",                // uc.js async + blockingmode=auto, and wrong region (.com vs eu)
		"S3": "youtube.com",          // live embed iframe
		"S4": "data-cookieconsent",   // gated iframe missing consent attr
		"S5": "platform.twitter.com", // ungated external embed script
		"S6": "createElement",        // hostile inline injector
		"S7": "katowice.example",     // third-party img
	}
	for id, substr := range wantFail {
		ch := checks[id]
		if ch.Status != report.Fail {
			t.Errorf("%s = %s (%s), want fail", id, ch.Status, ch.Detail)
		}
		if !strings.Contains(ch.Detail, substr) {
			t.Errorf("%s detail %q should mention %q", id, ch.Detail, substr)
		}
	}
}

func TestNoCookiebotAtAll(t *testing.T) {
	c := cfg()
	checks := Run([]byte("<html><body><p>hello</p></body></html>"), "https://tvs.example/", c)
	// GTM-only install is a structural weakness, not observed leakage: WARN, not FAIL
	if checks[0].ID != "S1" || checks[0].Status != report.Warn {
		t.Errorf("S1 on cookiebot-less page = %+v, want warn", checks[0])
	}
}

// Regression, tvs.pl 2026-07-13: a theme filter that APPENDS type="text/plain" to a
// script tag which already carries type="text/javascript" produces a duplicate
// attribute. The browser keeps the FIRST (spec: duplicate-attribute parse error, new
// one dropped) and executes the script — but x/net/html hands back both, so a
// last-wins attribute map reads "text/plain" and calls the tracker gated.
// The check must lie in neither direction, least of all the safe one.
func TestDuplicateTypeAttrIsNotGated(t *testing.T) {
	page := []byte(`<html><body>
	  <script type="text/javascript" src="https://video.onnetwork.tv/widget/w.php"
	          type="text/plain" data-cookieconsent="marketing"></script>
	</body></html>`)
	var s5c report.Check
	for _, ch := range Run(page, "https://tvs.example/", cfg()) {
		if ch.ID == "S5" {
			s5c = ch
		}
	}
	if s5c.Status != report.Fail {
		t.Errorf("S5 = %s (%s); a script whose first type= is text/javascript EXECUTES and must be reported ungated",
			s5c.Status, s5c.Detail)
	}
	if !strings.Contains(s5c.Detail, "onnetwork.tv") {
		t.Errorf("S5 detail %q should name the offending script", s5c.Detail)
	}
}

// uc.js restores data-src and data-cookieblock-src interchangeably when
// data-cookieconsent is present. The official Cookiebot WordPress plugin gates with
// data-src, so a plugin-gated page must read as gated, not as "0 gated / ungated".
func TestPluginStyleDataSrcCountsAsGated(t *testing.T) {
	page := []byte(`<html><head><script id="Cookiebot" src="https://consent.cookiebot.eu/uc.js"></script></head><body>
	  <iframe data-cookieconsent="marketing" data-src="https://www.youtube.com/embed/abc"></iframe>
	  <script data-cookieconsent="marketing" data-src="https://platform.twitter.com/widgets.js"></script>
	</body></html>`)
	checks := map[string]report.Check{}
	for _, ch := range Run(page, "https://tvs.example/", cfg()) {
		checks[ch.ID] = ch
	}
	if checks["S5"].Status != report.Pass {
		t.Errorf("S5 = %s (%s), want pass: src parked in data-src is never fetched", checks["S5"].Status, checks["S5"].Detail)
	}
	if checks["S3"].Status != report.Pass {
		t.Errorf("S3 = %s (%s), want pass: no live src", checks["S3"].Status, checks["S3"].Detail)
	}
	if checks["S4"].Status != report.Pass || !strings.Contains(checks["S4"].Detail, "1 gated") {
		t.Errorf("S4 = %s (%s), want pass counting 1 gated iframe", checks["S4"].Status, checks["S4"].Detail)
	}
}

// creators.spotify.com must be caught by the spotify.com pattern — pinning a single
// subdomain (open.spotify.com) let the sibling host through unnoticed.
func TestEmbedHostsMatchAnySubdomain(t *testing.T) {
	page := []byte(`<html><body>
	  <iframe src="https://creators.spotify.com/pod/show/x/embed"></iframe>
	  <iframe src="https://player.vimeo.com/video/1"></iframe>
	</body></html>`)
	var s3c report.Check
	for _, ch := range Run(page, "https://tvs.example/", cfg()) {
		if ch.ID == "S3" {
			s3c = ch
		}
	}
	if s3c.Status != report.Fail {
		t.Fatalf("S3 = %s (%s), want fail", s3c.Status, s3c.Detail)
	}
	for _, want := range []string{"creators.spotify.com", "player.vimeo.com"} {
		if !strings.Contains(s3c.Detail, want) {
			t.Errorf("S3 detail %q should list %s", s3c.Detail, want)
		}
	}
}
