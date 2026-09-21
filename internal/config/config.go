// Package config holds the per-site scan configuration. Defaults mirror the
// Inputs section of claude-check-process.md; a JSON file overrides them.
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
)

type Site struct {
	Domain          string   `json:"domain,omitempty"`
	CBID            string   `json:"cbid,omitempty"`
	Region          string   `json:"region,omitempty"` // "com" or "eu" — picks consent.cookiebot.<region>
	TrackerHosts    []string `json:"trackerHosts,omitempty"`
	EmbedHosts      []string `json:"embedHosts,omitempty"`
	CookieAllowlist []string `json:"cookieAllowlist,omitempty"` // cookie names allowed pre-consent
	ImageAllowHosts []string `json:"imageAllowHosts,omitempty"` // third-party img hosts that are fine (CDNs)
	UserAgent       string   `json:"userAgent,omitempty"`

	trackerRe, embedRe, imageAllowRe *regexp.Regexp
	trackerHostRe, trackerURLRe      *regexp.Regexp
	cookieAllowRe                    *regexp.Regexp
}

func Defaults() *Site {
	return &Site{
		Region: "com",
		// The list grew out of embed-heavy media sites, so it covered players and
		// ad exchanges but not the analytics and session-recording tags a small
		// business site actually runs. aldent.lublin.pl (2026-09-21) loaded
		// Microsoft Clarity — a session recorder, the most invasive thing on the
		// page — and Bing UET before consent, and R1 did not name either: the
		// check still failed on the Spotify pixel, so the verdict was right while
		// the evidence handed to the client was missing its worst item.
		//
		// googletagmanager.com is deliberately ABSENT. Loading gtm.js ahead of
		// consent is the documented Consent Mode design: the container arrives
		// with storage denied and the tags inside it decide. Listing it would fail
		// R1 on every correctly configured site. What must be judged is the tags
		// the container then fires — which is exactly what these hosts catch.
		TrackerHosts: []string{
			// embeds & ad exchanges
			`youtube\.com`, `ytimg`, `platform\.twitter`, `syndication\.(x|twitter)\.com`,
			`tiktok`, `ttwstatic`, `tiktokcdn`, `spotify`, `ustat\.info`, `openstat\.eu`,
			`doubleclick`, `googlesyndication`, `imasdk`, `onnetwork\.tv`,
			`googleadservices\.com`,
			// analytics & advertising tags
			`google-analytics\.com`, `analytics\.google\.com`,
			`connect\.facebook\.net`, `facebook\.com/tr`,
			`bat\.bing\.`, `clarity\.ms`,
			`snap\.licdn\.com`, `ads\.linkedin\.com`,
			`mc\.yandex\.`, `ct\.pinterest\.com`,
			// session recording / heatmaps
			`hotjar\.(com|io)`, `smartlook\.com`, `fullstory\.com`,
			`mouseflow\.com`, `luckyorange\.(com|net)`,
		},
		// Name the REGISTRABLE domain, not one subdomain: embeds move hosts freely
		// (open. vs creators.spotify.com, player.vimeo.com vs vimeo.com). Pinning a
		// subdomain makes the check silently miss the sibling — creators.spotify.com
		// slipped past `open\.spotify\.com` on tvs.pl for weeks.
		EmbedHosts: []string{
			`youtube\.com`, `youtu\.be`, `youtube-nocookie\.com`, `vimeo\.com`,
			`dailymotion\.com`, `twitter\.com`, `x\.com`, `facebook\.com/plugins`,
			`instagram\.com`, `tiktok\.com`, `spotify\.com`, `soundcloud\.com`,
			`google\.com/maps`, `onnetwork\.tv`,
		},
		CookieAllowlist: knownEssentialCookies,
		ImageAllowHosts: []string{`gravatar\.com`, `\.wp\.com`},
		UserAgent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 " +
			"(KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36",
	}
}

// Load returns Defaults overridden by the JSON file (path may be empty).
func Load(path string) (*Site, error) {
	s := Defaults()
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, s); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return s, nil
}

// DeriveDomain fills Domain from a page URL when the config didn't set it.
func (s *Site) DeriveDomain(pageURL string) {
	if s.Domain != "" {
		return
	}
	if u, err := url.Parse(pageURL); err == nil {
		s.Domain = strings.TrimPrefix(u.Hostname(), "www.")
	}
}

func (s *Site) TrackerRe() *regexp.Regexp {
	if s.trackerRe == nil {
		s.trackerRe = regexp.MustCompile("(?i)" + strings.Join(s.TrackerHosts, "|"))
	}
	return s.trackerRe
}

// IsTracker reports whether a URL contacts a tracker, matching each pattern
// against the part of the URL it actually names:
//
//   - a pattern with no "/" names a HOST and is tested against the host alone;
//   - a pattern containing "/" (facebook\.com/tr, google\.com/maps) names a
//     host and path, and is tested against the whole URL.
//
// TrackerRe tests the whole URL against everything, which is wrong for the bare
// words in the default list: `tiktok` and `spotify` match a path as happily as
// a host, so a studio's own article image at
// /blog/tiktok-marketing-for-studios/cover.jpg was counted as a tracker request
// (proved in runtime's truth_test.go, 2026-09-21). TrackerRe is kept for
// callers that match attribute values rather than resolved URLs.
func (s *Site) IsTracker(rawurl string) bool {
	host, ok := hostOf(rawurl)
	if !ok {
		return false // data:, blob:, about: and relative refs contact nobody
	}
	if re := s.compileSplit(&s.trackerHostRe, false); re != nil && re.MatchString(host) {
		return true
	}
	if re := s.compileSplit(&s.trackerURLRe, true); re != nil && re.MatchString(rawurl) {
		return true
	}
	return false
}

// compileSplit builds (once) the half of TrackerHosts that carries a path
// component, or the half that does not.
func (s *Site) compileSplit(dst **regexp.Regexp, withPath bool) *regexp.Regexp {
	if *dst != nil {
		return *dst
	}
	var parts []string
	for _, p := range s.TrackerHosts {
		if strings.Contains(p, "/") == withPath {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	*dst = regexp.MustCompile("(?i)" + strings.Join(parts, "|"))
	return *dst
}

// hostOf returns the lower-case host of an absolute http(s) URL.
func hostOf(rawurl string) (string, bool) {
	u, err := url.Parse(rawurl)
	if err != nil || u.Host == "" {
		return "", false
	}
	if u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	return strings.ToLower(u.Hostname()), true
}

// IsAllowedCookie reports whether a cookie name is allowed before consent.
//
// CookieAllowlist entries are PATTERNS matched against the whole name, not
// literals. A CMP's record routinely carries a site or category id
// (cookielawinfo-checkbox-necessary, cmplz_banner-status, cookiefirst-consent),
// so an exact-name list needed a new entry per vendor per variant and lost that
// race every time: aldent.lublin.pl's `cookieyes-consent`, holding consent:no,
// was reported as a cookie set before consent. One pattern covers a vendor.
func (s *Site) IsAllowedCookie(name string) bool {
	if s.cookieAllowRe == nil {
		if len(s.CookieAllowlist) == 0 {
			return false
		}
		s.cookieAllowRe = regexp.MustCompile(`(?i)^(` + strings.Join(s.CookieAllowlist, "|") + `)$`)
	}
	return s.cookieAllowRe.MatchString(name)
}

func (s *Site) EmbedRe() *regexp.Regexp {
	if s.embedRe == nil {
		s.embedRe = regexp.MustCompile("(?i)" + strings.Join(s.EmbedHosts, "|"))
	}
	return s.embedRe
}

func (s *Site) ImageAllowRe() *regexp.Regexp {
	if s.imageAllowRe == nil && len(s.ImageAllowHosts) > 0 {
		s.imageAllowRe = regexp.MustCompile("(?i)" + strings.Join(s.ImageAllowHosts, "|"))
	}
	return s.imageAllowRe
}

// FirstParty reports whether host is the site domain or a subdomain of it.
func (s *Site) FirstParty(host string) bool {
	host = strings.TrimPrefix(strings.TrimPrefix(host, "."), "www.")
	return host == s.Domain || strings.HasSuffix(host, "."+s.Domain)
}

// ConsentHost is the host uc.js is served from (region-dependent).
func (s *Site) ConsentHost() string {
	return "consent.cookiebot." + s.Region
}

// ChecklistURL is the auto-block checklist for this domain group.
func (s *Site) ChecklistURL() string {
	return fmt.Sprintf("https://consentcdn.cookiebot.%s/consentconfig/%s/%s/configuration.js",
		s.Region, s.CBID, s.Domain)
}
