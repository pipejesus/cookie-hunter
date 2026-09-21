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
// Entries are PATTERNS (see Site.IsAllowedCookie), anchored to the whole name,
// which is what lets one line cover a vendor whose record carries a site or
// category id — cookielawinfo-checkbox-necessary, cmplz_banner-status,
// cookiefirst-consent. The exact-name version needed a line per variant and
// lost that race on every site we looked at.
var knownEssentialCookies = []string{
	// sessions / CSRF
	"PHPSESSID", "laravel_session", "XSRF-TOKEN",
	"csrftoken", "JSESSIONID", "wordpress_test_cookie",
	"wp-settings-.*", "wp-settings-time-.*",
	// infrastructure: bot management and load balancing, strictly necessary
	"cf_clearance", "__cf_bm", "__cflb", "AWSALB.*", "incap_ses_.*", "visid_incap_.*",
	// CMP consent records
	"CookieConsent.*",                                               // Cookiebot
	"cookieyes-consent", "cookielawinfo-.*", "viewed_cookie_policy", // CookieYes / CLI
	"cmplz_.*", "complianz_.*", // Complianz
	"borlabs-cookie.*",                // Borlabs
	"real_cookie_banner-.*", "rcb-.*", // Real Cookie Banner
	"Optanon.*",                       // OneTrust
	"euconsent-v2", "eupubconsent-v2", // IAB TCF
	"moove_gdpr_popup",             // Moove GDPR
	"cookie_notice_accepted",       // Cookie Notice (dFactory)
	"_iub_cs-.*",                   // Iubenda
	"usercentrics.*", "ucSettings", // Usercentrics
	"termly-.*", "klaro", "wp_consent_.*", "CONSENTMGR",
	"CookieScriptConsent", // CookieScript
	"cookiefirst[a-z_-]*", // CookieFirst
	"cacsp[a-z_-]*",       // Cookies and Content Security Policy
	"consentmo[a-z_-]*",   // Consentmo
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
