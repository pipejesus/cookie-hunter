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
