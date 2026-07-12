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
	Domain          string   `json:"domain"`
	CBID            string   `json:"cbid"`
	Region          string   `json:"region"` // "com" or "eu" — picks consent.cookiebot.<region>
	TrackerHosts    []string `json:"trackerHosts"`
	EmbedHosts      []string `json:"embedHosts"`
	CookieAllowlist []string `json:"cookieAllowlist"` // cookie names allowed pre-consent
	ImageAllowHosts []string `json:"imageAllowHosts"` // third-party img hosts that are fine (CDNs)
	UserAgent       string   `json:"userAgent"`

	trackerRe, embedRe, imageAllowRe *regexp.Regexp
}

func Defaults() *Site {
	return &Site{
		Region: "com",
		TrackerHosts: []string{
			`youtube\.com`, `ytimg`, `platform\.twitter`, `syndication\.(x|twitter)\.com`,
			`tiktok`, `ttwstatic`, `tiktokcdn`, `spotify`, `ustat\.info`, `openstat\.eu`,
			`doubleclick`, `googlesyndication`, `imasdk`,
		},
		EmbedHosts: []string{
			`youtube\.com`, `youtu\.be`, `youtube-nocookie\.com`, `player\.vimeo\.com`,
			`platform\.twitter\.com`, `tiktok\.com`, `open\.spotify\.com`,
			`facebook\.com/plugins`, `soundcloud\.com`,
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
