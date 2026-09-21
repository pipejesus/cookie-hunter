package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"cookie-hunter/internal/config"
	"cookie-hunter/internal/report"
)

// These tests answer one question with a real browser and a fixture whose
// behaviour we control exactly: does cookie-hunter report what actually
// happened? Live sites cannot answer it — you never know what a third party
// was going to do. Here we know, so a disagreement is the tool's.
//
// Each assertion is labelled CLAIM (the tool must be right) or LIMIT (a known
// blind spot we are pinning down so it cannot be mistaken for a clean result).

func byID(checks []report.Check) map[string]report.Check {
	m := map[string]report.Check{}
	for _, c := range checks {
		m[c.ID] = c
	}
	return m
}

func hasCookie(cookies []string, name string) bool {
	for _, c := range cookies {
		if strings.HasPrefix(c, name+"@") {
			return true
		}
	}
	return false
}

func itemsContain(c report.Check, substr string) bool {
	for _, i := range c.Items {
		if strings.Contains(i, substr) {
			return true
		}
	}
	return false
}

// TestCookieJarTruth is the core of Greg's assumption: that a fresh real
// browser is asked which cookies exist before consent, and that the answer is
// the real jar rather than what the page admits to.
func TestCookieJarTruth(t *testing.T) {
	if !chromeAvailable() {
		t.Skip("no Chrome/Chromium on PATH")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// HttpOnly: invisible to document.cookie. If this shows up, the jar is
		// being read over CDP, which is the only way to see it.
		http.SetCookie(w, &http.Cookie{Name: "srv_httponly", Value: "1", HttpOnly: true, Path: "/"})
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
		<script>
		  document.cookie = "_ga=GA1.1.1296467412.1789999103; path=/";
		  document.cookie = "_ga_ABCD123456=GS2.1.s1789999103; path=/";
		  document.cookie = "cookieyes-consent=consentid:zzz,consent:no,analytics:no; path=/";
		  document.cookie = "PHPSESSID=abc123; path=/";
		  // fires well after the 6 s settle window — must NOT appear
		  setTimeout(function () {
		    document.cookie = "_hjSessionUser_999=late; path=/";
		  }, 15000);
		</script>
		</body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := config.Defaults()
	cfg.Domain = "127.0.0.1"

	res, err := Scan(context.Background(), cfg, srv.URL, false)
	if err != nil {
		t.Fatal(err)
	}
	k1 := byID(res.Checks)["K1"]
	t.Logf("pre-consent jar: %v", res.PreCookies)
	t.Logf("K1: %s | %s", k1.Status, k1.Detail)

	// CLAIM — a JS-written analytics cookie is seen. This is the whole premise.
	if !hasCookie(res.PreCookies, "_ga") {
		t.Errorf("CLAIM FAILED: _ga set via document.cookie was not in the jar: %v", res.PreCookies)
	}
	if !hasCookie(res.PreCookies, "_ga_ABCD123456") {
		t.Errorf("CLAIM FAILED: the GA4 per-property cookie was not in the jar: %v", res.PreCookies)
	}
	// CLAIM — an HttpOnly cookie is seen too, which document.cookie could not do.
	if !hasCookie(res.PreCookies, "srv_httponly") {
		t.Errorf("CLAIM FAILED: HttpOnly cookie missing — the jar is not being read over CDP: %v", res.PreCookies)
	}
	// CLAIM — K1 fails, and names the tracking cookie.
	if k1.Status != report.Fail {
		t.Errorf("CLAIM FAILED: K1 = %s, want fail with _ga in the jar", k1.Status)
	}
	if !itemsContain(k1, "_ga@") {
		t.Errorf("CLAIM FAILED: K1 items do not name _ga: %v", k1.Items)
	}
	// CLAIM — essentials and the consent record are not counted against the site.
	if itemsContain(k1, "cookieyes-consent") {
		t.Errorf("CLAIM FAILED: the consent record was reported as a pre-consent cookie: %v", k1.Items)
	}
	if itemsContain(k1, "PHPSESSID") {
		t.Errorf("CLAIM FAILED: a session cookie was reported as a pre-consent cookie: %v", k1.Items)
	}

	// LIMIT — a cookie written after the settle window is invisible. The run is
	// a 6 s snapshot, not a session. A tracker on a slow timer, or one that only
	// fires on scroll or on an interaction, reads as clean here.
	if hasCookie(res.PreCookies, "_hjSessionUser_999") {
		t.Log("NOTE: the late cookie was caught — settle window is wider than assumed; re-check the LIMIT note")
	} else {
		t.Log("LIMIT CONFIRMED: a cookie written 15 s after load is not seen (settle = 6 s). " +
			"A clean K1 means 'nothing in the first 6 seconds', not 'nothing ever'.")
	}
}

// TestRequestLeakTruth pins down R1/R2: which network requests count as
// leakage, which are correctly forgiven, and where the matcher is too eager.
func TestRequestLeakTruth(t *testing.T) {
	if !chromeAvailable() {
		t.Skip("no Chrome/Chromium on PATH")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// The URL matcher tests the whole URL string, so a local path carrying a
		// tracker's domain name reproduces a real hit exactly.
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
		<img src="/google-analytics.com/g/collect?v=2&tid=G-TEST&gcs=G100">
		<img src="/google-analytics.com/g/collect?v=2&tid=G-TEST&cid=123">
		<img src="/blog/tiktok-marketing-for-studios/cover.jpg">
		</body></html>`)
	})
	mux.HandleFunc("/google-analytics.com/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/gif")
	})
	mux.HandleFunc("/blog/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := config.Defaults()
	cfg.Domain = "127.0.0.1"

	res, err := Scan(context.Background(), cfg, srv.URL, false)
	if err != nil {
		t.Fatal(err)
	}
	m := byID(res.Checks)
	r1, r2 := m["R1"], m["R2"]
	t.Logf("R1: %s | %s", r1.Status, r1.Detail)
	t.Logf("R2: %s | %s", r2.Status, r2.Detail)

	// CLAIM — a Consent-Mode denied ping is NOT leakage and is excluded from R1.
	if itemsContain(r1, "gcs=G100") {
		t.Errorf("CLAIM FAILED: a gcs=G100 denied ping was counted as leakage: %v", r1.Items)
	}
	if !strings.Contains(r1.Detail, "denied pings") {
		t.Errorf("CLAIM FAILED: R1 detail does not account for the denied ping: %s", r1.Detail)
	}
	// CLAIM — the same request WITHOUT a consent signal is leakage.
	if !itemsContain(r1, "cid=123") {
		t.Errorf("CLAIM FAILED: a GA hit with no consent signal was not counted: %v", r1.Items)
	}
	if r1.Status != report.Fail {
		t.Errorf("CLAIM FAILED: R1 = %s, want fail", r1.Status)
	}
	// CLAIM — R2 judges GA hits by the consent signal, 1 bad of 2.
	if r2.Status != report.Fail || !strings.Contains(r2.Detail, "1 of 2") {
		t.Errorf("CLAIM FAILED: R2 = %s | %s, want fail '1 of 2'", r2.Status, r2.Detail)
	}

	// LIMIT — TrackerHosts are matched against the WHOLE URL, and several
	// entries are bare words (`tiktok`, `spotify`). An ordinary article path
	// therefore reads as a tracker request.
	if itemsContain(r1, "tiktok-marketing") {
		t.Log("LIMIT CONFIRMED: /blog/tiktok-marketing-for-studios/cover.jpg counted as a tracker " +
			"request — bare-word TrackerHosts match the path, not just the host.")
	} else {
		t.Log("NOTE: the article path did not match; the bare-word entries may have been tightened")
	}
}

// TestFreshProfilePerRun guards the premise under everything else: if one run
// could see the previous run's cookies, every verdict would be noise. Two scans
// of a page that sets a cookie must each start empty.
func TestFreshProfilePerRun(t *testing.T) {
	if !chromeAvailable() {
		t.Skip("no Chrome/Chromium on PATH")
	}

	// The proof is server-side: what did the browser SEND us each time? A leaked
	// profile would return _ga on the second navigation.
	var mu sync.Mutex
	var received [][]string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Only the top-level navigation counts. Chrome also fetches /favicon.ico,
		// which this handler would otherwise catch — and by then the browser
		// legitimately holds the cookie the navigation just set, which looks
		// exactly like a leaked profile if you don't separate the two.
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		var names []string
		for _, c := range r.Cookies() {
			names = append(names, c.Name)
		}
		mu.Lock()
		received = append(received, names)
		mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "_ga", Value: "GA1.1.persist", Path: "/"})
		fmt.Fprint(w, `<!DOCTYPE html><html><body>run</body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := config.Defaults()
	cfg.Domain = "127.0.0.1"

	for i := 0; i < 2; i++ {
		res, err := Scan(context.Background(), cfg, srv.URL, false)
		if err != nil {
			t.Fatal(err)
		}
		if !hasCookie(res.PreCookies, "_ga") {
			t.Fatalf("run %d: fixture did not set _ga, so the test proves nothing", i+1)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) < 2 {
		t.Fatalf("server saw %d navigations, want 2", len(received))
	}
	// CLAIM — every run starts from an empty jar (P8). Without this, one scan
	// would poison the next and every pre-consent verdict would be noise.
	for i, names := range received {
		if len(names) != 0 {
			t.Errorf("CLAIM FAILED: run %d arrived carrying cookies %v — the profile is not fresh", i+1, names)
		}
	}
	t.Logf("both runs arrived with an empty jar (%d navigations, each sent nothing)", len(received))
}
