// Package checklist implements Phase 1 (checks C1–C4): the per-domain
// auto-block checklist served as configuration.js. Cat semantics (verified
// against uc.js source): 2=preferences, 3=statistics, 4=marketing are blocked
// pre-consent; 1 (necessary) and 5 (unclassified) are NOT blocked. Matching is
// exact-URL, so the checklist only covers what the Cookiebot scanner crawled.
package checklist

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"cookie-hunter/internal/config"
	"cookie-hunter/internal/report"
	"cookie-hunter/internal/web"
)

type Entry struct {
	Type string `json:"type"`
	URL  string `json:"url"`
	Cats []int  `json:"cats"`
}

var (
	pushRe = regexp.MustCompile(`(?s)tags\.push\(\s*\{(.*?)\}\s*\)`)
	typeRe = regexp.MustCompile(`["']?type["']?\s*:\s*["']([^"']*)`)
	urlRe  = regexp.MustCompile(`["']?url["']?\s*:\s*["']([^"']*)`)
	catRe  = regexp.MustCompile(`["']?cat["']?\s*:\s*\[([^\]]*)\]`)
)

func Parse(raw []byte) []Entry {
	var entries []Entry
	for _, m := range pushRe.FindAllSubmatch(raw, -1) {
		body := m[1]
		var e Entry
		if t := typeRe.FindSubmatch(body); t != nil {
			e.Type = string(t[1])
		}
		if u := urlRe.FindSubmatch(body); u != nil {
			e.URL = string(u[1])
		}
		if c := catRe.FindSubmatch(body); c != nil {
			for _, part := range strings.Split(string(c[1]), ",") {
				if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
					e.Cats = append(e.Cats, n)
				}
			}
		}
		entries = append(entries, e)
	}
	return entries
}

func (e Entry) blocked() bool {
	for _, c := range e.Cats {
		if c >= 2 && c <= 4 {
			return true
		}
	}
	return false
}

// Fetch downloads the checklist; the raw bytes go to the snapshot dir.
func Fetch(cfg *config.Site) (raw []byte, status int, err error) {
	return web.Get(cfg.ChecklistURL(), cfg.UserAgent)
}

// Run evaluates C1–C4. prevRaw is the configuration.js from the previous
// snapshot (nil = no previous run).
func Run(raw []byte, status int, cfg *config.Site, prevRaw []byte) []report.Check {
	if status == 404 {
		c := report.New("C1", false, "checklist HTTP 404 — domain never scanned by Cookiebot")
		return []report.Check{c,
			report.Skipped("C2", "no checklist"), report.Skipped("C3", "no checklist"), report.Skipped("C4", "no checklist")}
	}
	if status != 200 {
		c := report.Skipped("C1", fmt.Sprintf("checklist HTTP %d — cannot evaluate", status))
		return []report.Check{c,
			report.Skipped("C2", "no checklist"), report.Skipped("C3", "no checklist"), report.Skipped("C4", "no checklist")}
	}

	entries := Parse(raw)
	checks := []report.Check{
		report.New("C1", true, fmt.Sprintf("checklist fetched, %d entries", len(entries))),
	}

	// C2 — known trackers marked "don't block" (cat 1/5 only)
	var unblocked []string
	for _, e := range entries {
		if e.URL != "" && cfg.TrackerRe().MatchString(e.URL) && !e.blocked() {
			unblocked = append(unblocked, fmt.Sprintf("%s cat:%v", e.URL, e.Cats))
		}
	}
	checks = append(checks, report.New("C2",
		len(unblocked) == 0,
		fmt.Sprintf("trackers NOT auto-blocked: %d %s", len(unblocked), sample(unblocked))))

	// C3 — first-party essentials that WOULD be blocked (breaks the site when auto-block activates)
	var firstPartyBlocked []string
	for _, e := range entries {
		if e.URL == "" || !e.blocked() {
			continue
		}
		if u, err := url.Parse(e.URL); err == nil && cfg.FirstParty(u.Hostname()) {
			firstPartyBlocked = append(firstPartyBlocked, fmt.Sprintf("%s cat:%v", e.URL, e.Cats))
		}
	}
	checks = append(checks, report.New("C3",
		len(firstPartyBlocked) == 0,
		fmt.Sprintf("first-party files marked blockable: %d %s", len(firstPartyBlocked), sample(firstPartyBlocked))))

	// C4 — drift vs previous snapshot (informational, never fails)
	switch {
	case prevRaw == nil:
		checks = append(checks, report.Skipped("C4", "no previous snapshot to diff against"))
	case bytes.Equal(raw, prevRaw):
		checks = append(checks, report.New("C4", true, "unchanged since previous snapshot"))
	default:
		checks = append(checks, report.New("C4", true,
			fmt.Sprintf("CHANGED since previous snapshot: %d -> %d entries", len(Parse(prevRaw)), len(entries))))
	}
	return checks
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
