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
}

func Defaults() *Site {
	return &Site{
		Region: "com",
		TrackerHosts: []string{
			`youtube\.com`, `ytimg`, `platform\.twitter`, `syndication\.(x|twitter)\.com`,
			`tiktok`, `ttwstatic`, `tiktokcdn`, `spotify`, `ustat\.info`, `openstat\.eu`,
			`doubleclick`, `googlesyndication`, `imasdk`, `onnetwork\.tv`,
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
		CookieAllowlist: []string{"CookieConsent"},
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
