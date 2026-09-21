// Package runtime implements Phases 2–3: real Chrome over CDP via chromedp.
// Pre-consent checks R1–R3 (network), D1–D3 (DOM), K1 (cookie jar); with
// consent enabled, post-consent restore checks A1–A4.
//
// Non-negotiables from the spec's pitfalls:
//   - P8: fresh ExecAllocator (own temp profile) per URL — a leftover
//     CookieConsent cookie turns every pre-consent check into a false pass.
//   - P4: cookies via CDP storage.GetCookies (browser-wide jar incl. HttpOnly
//     and third-party), never document.cookie.
//   - interval trackers: observe ≥6 s after load (openstat fired on a 5 s tick).
//   - P3: blocking is judged by DOM attributes, leakage by network — lazy
//     loading makes the two counts legitimately disagree.
package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"

	"cookie-hunter/internal/config"
	"cookie-hunter/internal/report"
)

const settle = 6 * time.Second

type probe struct {
	HasCookiebot      bool     `json:"hasCookiebot"`
	ConsentMethod     *string  `json:"consentMethod"`
	Preferences       bool     `json:"preferences"`
	Statistics        bool     `json:"statistics"`
	Marketing         bool     `json:"marketing"`
	BannerVisible     bool     `json:"bannerVisible"`
	GatedScripts      int      `json:"gatedScripts"`
	GatedIframes      int      `json:"gatedIframes"`
	UnrestoredIframes int      `json:"unrestoredIframes"`
	LiveEmbedIframes  []string `json:"liveEmbedIframes"`
}

// Result carries everything observed; raw parts go into the snapshot dir.
type Result struct {
	Checks       []report.Check `json:"checks"`
	PreRequests  []string       `json:"preRequests"`
	PostRequests []string       `json:"postRequests,omitempty"`
	PreCookies   []string       `json:"preCookies"` // name@domain
	PostCookies  []string       `json:"postCookies,omitempty"`
	Pre          probe          `json:"pre"`
	Post         *probe         `json:"post,omitempty"`
}

func probeJS(cfg *config.Site) string {
	return fmt.Sprintf(`(() => {
	  const c = (window.Cookiebot && window.Cookiebot.consent) || null;
	  const re = new RegExp(%q, "i");
	  return {
	    hasCookiebot: !!window.Cookiebot,
	    consentMethod: c ? c.method : null,
	    preferences: !!(c && c.preferences),
	    statistics: !!(c && c.statistics),
	    marketing: !!(c && c.marketing),
	    bannerVisible: !!document.querySelector('#CybotCookiebotDialog'),
	    gatedScripts: document.querySelectorAll('script[type="text/plain"], script[data-src][data-cookieconsent]').length,
	    gatedIframes: document.querySelectorAll('iframe[data-cookieblock-src], iframe[data-src]').length,
	    unrestoredIframes: document.querySelectorAll('iframe[data-cookieblock-src]:not([src]), iframe[data-src]:not([src])').length,
	    liveEmbedIframes: [...document.querySelectorAll('iframe[src]')].map(f => f.src).filter(s => re.test(s)),
	  };
	})()`, strings.Join(cfg.EmbedHosts, "|"))
}

// Scan drives one URL in a fresh browser profile.
// Headed shows the browser window (default: headless). Package-level because
// it's a global run mode, not a per-site setting.
var Headed bool

func Scan(parent context.Context, cfg *config.Site, pageURL string, withConsent bool) (*Result, error) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
	defer cancel()

	// Fresh profile per URL (P8). chromedp creates a temp user-data-dir when
	// none is given and removes it on cancel.
	opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.UserAgent(cfg.UserAgent))
	if Headed {
		opts = append(opts, chromedp.Flag("headless", false))
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()
	// quiet chromedp's "unhandled node event" noise on newer Chrome builds
	tabCtx, cancelTab := chromedp.NewContext(allocCtx,
		chromedp.WithErrorf(func(string, ...interface{}) {}))
	defer cancelTab()

	res := &Result{}
	var mu sync.Mutex
	var requests []string
	// Listener attached before navigation so nothing is missed.
	chromedp.ListenTarget(tabCtx, func(ev interface{}) {
		if e, ok := ev.(*network.EventRequestWillBeSent); ok {
			mu.Lock()
			requests = append(requests, e.Request.URL)
			mu.Unlock()
		}
	})

	var cookies []*network.Cookie
	err := chromedp.Run(tabCtx,
		network.Enable(),
		chromedp.Navigate(pageURL),
		chromedp.Sleep(settle),
		chromedp.Evaluate(probeJS(cfg), &res.Pre),
		chromedp.ActionFunc(func(c context.Context) error {
			var err error
			cookies, err = storage.GetCookies().Do(c)
			return err
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("pre-consent run: %w", err)
	}
	mu.Lock()
	res.PreRequests = append([]string(nil), requests...)
	mu.Unlock()
	res.PreCookies = cookieNames(cookies)

	res.Checks = preConsentChecks(cfg, res, cookies)

	if !withConsent {
		return res, nil
	}

	// Phase 3 — accept everything via the official API (identical to the banner
	// button). NOTE: writes a real consent-log entry in the client's account.
	preCount := len(res.PreRequests)
	var submitted bool
	post := probe{}
	err = chromedp.Run(tabCtx,
		chromedp.Evaluate(`window.Cookiebot ? (window.Cookiebot.submitCustomConsent(true, true, true), true) : false`, &submitted),
		chromedp.Sleep(settle),
		chromedp.Evaluate(probeJS(cfg), &post),
		chromedp.ActionFunc(func(c context.Context) error {
			var err error
			cookies, err = storage.GetCookies().Do(c)
			return err
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("post-consent run: %w", err)
	}
	res.Post = &post
	mu.Lock()
	res.PostRequests = append([]string(nil), requests[preCount:]...)
	mu.Unlock()
	res.PostCookies = cookieNames(cookies)

	res.Checks = append(res.Checks, postConsentChecks(cfg, res, submitted)...)
	return res, nil
}

func cookieNames(cookies []*network.Cookie) []string {
	var out []string
	for _, c := range cookies {
		out = append(out, c.Name+"@"+c.Domain)
	}
	return out
}

func preConsentChecks(cfg *config.Site, res *Result, cookies []*network.Cookie) []report.Check {
	var checks []report.Check

	// R1 — zero requests to tracker hosts. Requests carrying gcs=G100 are
	// Consent Mode v2 denied-state pings (cookieless by design) — Google tags
	// send those on purpose when consent is denied, so they are not leakage;
	// same semantics R2 applies to GA hits.
	var trackerHits, deniedPings []string
	for _, r := range matching(res.PreRequests, cfg, true) {
		if strings.Contains(r, "gcs=G100") {
			deniedPings = append(deniedPings, r)
		} else {
			trackerHits = append(trackerHits, r)
		}
	}
	r1detail := fmt.Sprintf("tracker requests pre-consent: %d %s", len(trackerHits), sample(trackerHits))
	if len(deniedPings) > 0 {
		r1detail += fmt.Sprintf(" (+%d consent-mode denied pings, gcs=G100 — not leakage)", len(deniedPings))
	}
	checks = append(checks, report.NewList("R1", len(trackerHits) == 0, r1detail, trackerHits))

	// R2 — Consent Mode denied on every GA hit
	var gaBad, gaAll []string
	for _, r := range res.PreRequests {
		if strings.Contains(r, "google-analytics.com/") || strings.Contains(r, "analytics.google.com/") {
			gaAll = append(gaAll, r)
			if !strings.Contains(r, "gcs=G100") {
				gaBad = append(gaBad, r)
			}
		}
	}
	if len(gaAll) == 0 {
		checks = append(checks, report.Skipped("R2", "no GA requests observed"))
	} else {
		checks = append(checks, report.NewList("R2", len(gaBad) == 0,
			fmt.Sprintf("GA hits without gcs=G100: %d of %d %s", len(gaBad), len(gaAll), sample(gaBad)), gaBad))
	}

	// R3 — the CMP itself must load (blocking everything incl. uc.js is also a
	// failure). Region-agnostic on purpose: whether it's the RIGHT region host
	// is S1's job; here we only ask "did a CMP arrive at all".
	cmpHit := ""
	for _, r := range res.PreRequests {
		if strings.Contains(r, ".cookiebot.") || strings.Contains(strings.ToLower(r), "usercentrics") {
			cmpHit = r
			break
		}
	}
	checks = append(checks, report.New("R3", cmpHit != "", "uc.js/CMP loaded: "+firstOr(cmpHit, "no request to any cookiebot/usercentrics host")))

	// D1 — no consent recorded
	d1 := res.Pre.HasCookiebot && res.Pre.ConsentMethod == nil && !res.Pre.Statistics && !res.Pre.Marketing
	detail := fmt.Sprintf("method=%s statistics=%v marketing=%v", strOrNull(res.Pre.ConsentMethod), res.Pre.Statistics, res.Pre.Marketing)
	if !res.Pre.HasCookiebot {
		detail = "window.Cookiebot missing"
	}
	checks = append(checks, report.New("D1", d1, detail))

	// D2 — banner shown (consent can't be "prior" if it's never asked for)
	checks = append(checks, report.New("D2", res.Pre.BannerVisible,
		fmt.Sprintf("bannerVisible=%v", res.Pre.BannerVisible)))

	// D3 — zero live embed iframes
	checks = append(checks, report.New("D3", len(res.Pre.LiveEmbedIframes) == 0,
		fmt.Sprintf("live embed iframes: %d %s (gated: %d iframes, %d scripts)",
			len(res.Pre.LiveEmbedIframes), sample(res.Pre.LiveEmbedIframes), res.Pre.GatedIframes, res.Pre.GatedScripts)))

	// K1 — cookie jar ⊆ allowlist
	allow := map[string]bool{}
	for _, n := range cfg.CookieAllowlist {
		allow[n] = true
	}
	var badCookies []string
	for _, c := range cookies {
		if !allow[c.Name] {
			badCookies = append(badCookies, c.Name+"@"+c.Domain)
		}
	}
	checks = append(checks, report.NewList("K1", len(badCookies) == 0,
		fmt.Sprintf("non-allowlisted cookies pre-consent: %d %s", len(badCookies), sample(badCookies)), badCookies))

	return checks
}

func postConsentChecks(cfg *config.Site, res *Result, submitted bool) []report.Check {
	if !submitted || res.Post == nil {
		return []report.Check{report.Skipped("A1", "Cookiebot object missing — consent could not be submitted"),
			report.Skipped("A2", ""), report.Skipped("A3", ""), report.Skipped("A4", "")}
	}
	var checks []report.Check
	post := res.Post

	// A1 — consent recorded as explicit, all categories true
	a1 := post.ConsentMethod != nil && *post.ConsentMethod == "explicit" &&
		post.Preferences && post.Statistics && post.Marketing
	checks = append(checks, report.New("A1",
		a1, fmt.Sprintf("method=%s preferences=%v statistics=%v marketing=%v",
			strOrNull(post.ConsentMethod), post.Preferences, post.Statistics, post.Marketing)))

	// A2 — full restore: no still-gated iframes; live count matches what was gated
	a2 := post.UnrestoredIframes == 0 && len(post.LiveEmbedIframes) == res.Pre.GatedIframes
	checks = append(checks, report.New("A2",
		a2, fmt.Sprintf("unrestored=%d, live now=%d, was gated=%d",
			post.UnrestoredIframes, len(post.LiveEmbedIframes), res.Pre.GatedIframes)))

	// A3 — revived scripts/iframes actually load
	if res.Pre.GatedIframes+res.Pre.GatedScripts == 0 {
		checks = append(checks, report.Skipped("A3", "nothing was gated pre-consent"))
	} else {
		revived := matching(res.PostRequests, cfg, false)
		checks = append(checks, report.New("A3", len(revived) > 0,
			fmt.Sprintf("post-consent embed/tracker requests: %d %s", len(revived), sample(revived))))
	}

	// A4 — analytics starts (_ga cookies appear)
	hasGA := false
	for _, c := range res.PostCookies {
		if strings.HasPrefix(c, "_ga") {
			hasGA = true
			break
		}
	}
	checks = append(checks, report.New("A4", hasGA, fmt.Sprintf("_ga cookie present: %v", hasGA)))

	return checks
}

// matching returns request URLs hitting tracker (and optionally embed) hosts,
// excluding the CMP's own hosts.
func matching(requests []string, cfg *config.Site, trackerOnly bool) []string {
	var out []string
	for _, r := range requests {
		if strings.Contains(r, ".cookiebot.") {
			continue
		}
		if cfg.IsTracker(r) || (!trackerOnly && cfg.EmbedRe().MatchString(r)) {
			out = append(out, r)
		}
	}
	return out
}

func firstOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func strOrNull(s *string) string {
	if s == nil {
		return "null"
	}
	return *s
}

func sample(items []string) string {
	if len(items) == 0 {
		return ""
	}
	if len(items) > 5 {
		items = append(items[:5:5], "…")
	}
	return "[" + strings.Join(items, ", ") + "]"
}
