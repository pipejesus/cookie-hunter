package config

import "testing"

// A tracker pattern names a host. Matching it against the whole URL made a
// studio's own article image read as a tracker request, because `tiktok` and
// `spotify` are bare words in the default list.
func TestIsTrackerMatchesHostNotPath(t *testing.T) {
	s := Defaults()
	s.Domain = "example.pl"

	tracker := []string{
		"https://analytics.tiktok.com/i18n/pixel/events.js",
		"https://pixel.byspotify.com/ping.min.js",
		"https://www.clarity.ms/tag/uet/123",
		"https://bat.bing.com/bat.js",
		"https://connect.facebook.net/pl_PL/all.js",
		"https://static.hotjar.com/c/hotjar-1.js",
		"https://ad.doubleclick.net/ccm/s/collect",
		"https://www.facebook.com/tr?id=1&ev=PageView", // pattern with a path
	}
	notTracker := []string{
		"https://example.pl/blog/tiktok-marketing-for-studios/cover.jpg",
		"https://example.pl/wp-content/uploads/spotify-playlist-hero.png",
		"https://example.pl/o-nas/clarity-w-praktyce/",
		"https://example.pl/hotjar-czy-warto.html",
		"data:image/png;base64,iVBORw0KGgo=",
		"/relative/path/tiktok.jpg",
		"https://www.facebook.com/naszestudio", // the page, not the pixel endpoint
	}
	for _, u := range tracker {
		if !s.IsTracker(u) {
			t.Errorf("missed a real tracker request: %s", u)
		}
	}
	for _, u := range notTracker {
		if s.IsTracker(u) {
			t.Errorf("false positive — the site's own content counted as a tracker: %s", u)
		}
	}
}

// The allowlist holds PATTERNS so one line covers a vendor whose record carries
// a site or category id. Exact names lost that race on every site we looked at.
func TestIsAllowedCookiePatterns(t *testing.T) {
	s := Defaults()

	allowed := []string{
		"PHPSESSID", "wordpress_test_cookie", "wp-settings-time-1",
		"cf_clearance", "__cf_bm",
		"CookieConsent", "CookieConsentBulkTicket",
		"cookieyes-consent", "cookielawinfo-checkbox-necessary", "viewed_cookie_policy",
		"cmplz_banner-status", "cmplz_consented_services",
		"OptanonAlertBoxClosed", "OptanonConsent",
		"CookieScriptConsent", "cookiefirst-consent", "cacsp-consent", "consentmo_gdpr",
		"borlabs-cookie", "_iub_cs-1234", "euconsent-v2",
	}
	denied := []string{
		"_ga", "_ga_ABC123", "_gcl_au", "_fbp", "_clck", "_uetsid",
		"_hjSessionUser_123", "_pk_id.1.abcd", "__spdt", "YSC",
		"breakdance_view_count", "pys_start_session", "sbjs_first",
	}
	for _, n := range allowed {
		if !s.IsAllowedCookie(n) {
			t.Errorf("essential/consent cookie reported as a pre-consent leak: %s", n)
		}
	}
	for _, n := range denied {
		if s.IsAllowedCookie(n) {
			t.Errorf("tracking cookie wrongly allowlisted: %s", n)
		}
	}
}
