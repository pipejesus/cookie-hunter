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
//
// A CMP's own consent record belongs here by definition: without it the banner
// cannot remember a refusal and would have to ask again on every page. Carrying
// only Cookiebot's `CookieConsent` made K1 report every other vendor's record
// as a leak — on aldent.lublin.pl (2026-09-21) that was `cookieyes-consent`,
// holding `consent:no`, i.e. the proof the visitor refused, counted as evidence
// against the site.
var knownEssentialCookies = []string{
	// sessions / CSRF
	"PHPSESSID", "laravel_session", "XSRF-TOKEN",
	"csrftoken", "JSESSIONID", "wordpress_test_cookie",
	// CMP consent records
	"CookieConsent", "CookieConsentBulkTicket", // Cookiebot
	"cookieyes-consent", "viewed_cookie_policy", // CookieYes / GDPR Cookie Consent
	"cmplz_banner-status", "cmplz_consented_services", // Complianz
	"borlabs-cookie",                          // Borlabs
	"real_cookie_banner",                      // Real Cookie Banner
	"OptanonAlertBoxClosed", "OptanonConsent", // OneTrust
	"euconsent-v2", "eupubconsent-v2", // IAB TCF
	"moove_gdpr_popup",       // Moove GDPR
	"cookie_notice_accepted", // Cookie Notice (dFactory)
	"_iub_cs-s",              // Iubenda
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
