package config

import "regexp"

var (
	cbidAttrRe  = regexp.MustCompile(`data-cbid="([0-9a-fA-F-]{36})"`)
	cbidParamRe = regexp.MustCompile(`[?&]cbid=([0-9a-fA-F-]{36})`)
	regionRe    = regexp.MustCompile(`consent\.cookiebot\.(eu|com)`)
)

// Well-known session-essential cookies, safe pre-consent. Anything not on the
// site's allowlist still fails K1 — extending the list is a reviewed human
// decision, never automatic.
var knownEssentialCookies = []string{
	"CookieConsent", "PHPSESSID", "laravel_session", "XSRF-TOKEN",
	"csrftoken", "JSESSIONID", "wordpress_test_cookie",
}

// Detect extracts the mechanical parts of a site config from a page's raw
// HTML: cbid (data-cbid attribute or uc.js ?cbid= param) and region (which
// consent.cookiebot host serves uc.js). The cookie allowlist is seeded with
// well-known essentials only.
func Detect(raw []byte, pageURL string) *Site {
	s := &Site{Region: "com", CookieAllowlist: knownEssentialCookies}
	s.DeriveDomain(pageURL)
	if m := cbidAttrRe.FindSubmatch(raw); m != nil {
		s.CBID = string(m[1])
	} else if m := cbidParamRe.FindSubmatch(raw); m != nil {
		s.CBID = string(m[1])
	}
	if m := regionRe.FindSubmatch(raw); m != nil {
		s.Region = string(m[1])
	}
	return s
}
