package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"cookie-hunter/internal/config"
	"cookie-hunter/internal/report"
)

// Integration test: drives real Chrome against a local fixture with one gated
// and one live iframe. Verifies the machinery (network capture, DOM probe,
// cookie jar) — not a real CMP, so D1/D2/R3 are expected to fail here.
func TestScanFixture(t *testing.T) {
	if !chromeAvailable() {
		t.Skip("no Chrome/Chromium on PATH")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "test_tracker", Value: "1"})
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<iframe data-cookieblock-src="https://www.youtube.com/embed/gated" data-cookieconsent="marketing"></iframe>
			<iframe src="/local-embed"></iframe>
			<script type="text/plain" data-cookieconsent="marketing">var x=1;</script>
			<img src="/pixel.gif">
		</body></html>`)
	})
	mux.HandleFunc("/local-embed", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html><body>embed</body></html>")
	})
	mux.HandleFunc("/pixel.gif", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/gif")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := config.Defaults()
	cfg.Domain = "127.0.0.1"

	res, err := Scan(context.Background(), cfg, srv.URL, false)
	if err != nil {
		t.Fatal(err)
	}

	if res.Pre.GatedIframes != 1 {
		t.Errorf("gatedIframes = %d, want 1", res.Pre.GatedIframes)
	}
	if res.Pre.GatedScripts != 1 {
		t.Errorf("gatedScripts = %d, want 1", res.Pre.GatedScripts)
	}
	if len(res.Pre.LiveEmbedIframes) != 0 {
		t.Errorf("liveEmbedIframes = %v, want none (local iframe doesn't match embedHosts)", res.Pre.LiveEmbedIframes)
	}
	// network capture saw the page and its subresources
	var sawPixel bool
	for _, r := range res.PreRequests {
		if strings.HasSuffix(r, "/pixel.gif") {
			sawPixel = true
		}
	}
	if !sawPixel {
		t.Errorf("network capture missed /pixel.gif; got %v", res.PreRequests)
	}
	// CDP cookie jar sees the HttpOnly-ish server cookie (P4)
	found := false
	for _, c := range res.PreCookies {
		if strings.HasPrefix(c, "test_tracker@") {
			found = true
		}
	}
	if !found {
		t.Errorf("cookie jar = %v, want test_tracker present", res.PreCookies)
	}
	// K1 must fail on the non-allowlisted cookie — checks wired end to end
	byID := map[string]report.Check{}
	for _, c := range res.Checks {
		byID[c.ID] = c
	}
	if byID["K1"].Status != report.Fail {
		t.Errorf("K1 = %+v, want fail on test_tracker", byID["K1"])
	}
	if byID["R1"].Status != report.Pass {
		t.Errorf("R1 = %+v, want pass (no tracker hosts contacted)", byID["R1"])
	}
	if byID["D1"].Status != report.Fail { // no Cookiebot object on the fixture
		t.Errorf("D1 = %+v, want fail (window.Cookiebot missing)", byID["D1"])
	}
}

func chromeAvailable() bool {
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}
	return false
}
