// Package static implements Phase 0 (checks S1–S7): raw-HTML checks, no browser.
// Attributes are read from the parsed tree, never grepped — pitfall P1:
// data-cookieblock-src="…" contains the substring src="…", so a naive regex
// counts every gated iframe as a live one.
package static

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"

	"cookie-hunter/internal/config"
	"cookie-hunter/internal/report"
)

var validConsentValues = map[string]bool{
	"preferences": true, "statistics": true, "marketing": true, "ignore": true,
}

// uc.js restores data-src and data-cookieblock-src interchangeably, provided
// data-cookieconsent is present (verified against live uc.js: it tests
// hasAttribute("data-cookieconsent") && (data-src || data-cookieblock-src)).
// The official Cookiebot WordPress plugin gates with data-src, hand-written markup
// usually with data-cookieblock-src — both must count as gated or the checks are
// blind to half the ecosystem.
func gatedSrc(attrs map[string]string) (string, bool) {
	for _, k := range []string{"data-cookieblock-src", "data-src"} {
		if v, ok := attrs[k]; ok {
			return v, true
		}
	}
	return "", false
}

// Heuristic for hostile inline scripts (pitfall P6): vendors like ustat/openstat
// assemble their URL character-by-character, so never grep for names — detect the
// injection behavior instead.
var injectorRe = regexp.MustCompile(`createElement\(\s*["']script["']\s*\)`)
var inlineWhitelistRe = regexp.MustCompile(`googletagmanager\.com|gtag\(|dataLayer`)

type element struct {
	tag   string
	attrs map[string]string
	text  string
}

// Run executes S1–S7 against one page's raw HTML.
func Run(raw []byte, pageURL string, cfg *config.Site) []report.Check {
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return []report.Check{{ID: "S0", Status: report.Fail, Detail: "HTML parse error: " + err.Error()}}
	}
	var iframes, scripts, imgs []element
	walk(doc, func(n *html.Node) {
		switch n.Data {
		case "iframe", "script", "img":
			el := element{tag: n.Data, attrs: map[string]string{}}
			for _, a := range n.Attr {
				// FIRST occurrence wins — duplicate attributes are a parse error and
				// the browser keeps the first (HTML spec, tree construction). x/net/html
				// hands us both, so a naive last-wins map inverts browser behaviour and
				// the check lies in the SAFE direction: a tag written
				//   <script type="text/javascript" src=… type="text/plain" data-cookieconsent=…>
				// still executes, but last-wins would read type="text/plain" and call it gated.
				// Real bug, seen in the wild (tvs.pl, 2026-07-13).
				k := strings.ToLower(a.Key)
				if _, seen := el.attrs[k]; seen {
					continue
				}
				el.attrs[k] = a.Val
			}
			if n.Data == "script" && n.FirstChild != nil {
				el.text = n.FirstChild.Data
			}
			switch n.Data {
			case "iframe":
				iframes = append(iframes, el)
			case "script":
				scripts = append(scripts, el)
			case "img":
				imgs = append(imgs, el)
			}
		}
	})

	return []report.Check{
		s1(raw, scripts, cfg),
		s2(raw),
		s3(iframes, cfg),
		s4(iframes),
		s5(scripts, cfg),
		s6(scripts),
		s7(imgs, cfg),
	}
}

func walk(n *html.Node, fn func(*html.Node)) {
	if n.Type == html.ElementNode {
		fn(n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}

// S1 — Cookiebot present in source; if so, the uc.js tag must be synchronous
// (no async/defer) when auto-blocking is intended, on the right region host.
func s1(raw []byte, scripts []element, cfg *config.Site) report.Check {
	count := bytes.Count(bytes.ToLower(raw), []byte("cookiebot"))
	if count == 0 {
		// Structural weakness, not observed leakage — the runtime checks (R/D/K)
		// are the truth-tellers for whether blocking actually works.
		return report.Warning("S1", "no 'cookiebot' in source — GTM-only install: banner late, no auto-block possible")
	}
	for _, s := range scripts {
		src := s.attrs["src"]
		if !strings.Contains(src, "cookiebot.") || !strings.Contains(src, "uc.js") {
			continue
		}
		var problems []string
		if !strings.Contains(src, cfg.ConsentHost()) {
			problems = append(problems, fmt.Sprintf("wrong region host (want %s): %s", cfg.ConsentHost(), src))
		}
		_, async := s.attrs["async"]
		_, deferred := s.attrs["defer"]
		if s.attrs["data-blockingmode"] == "auto" && (async || deferred) {
			problems = append(problems, "uc.js has async/defer with data-blockingmode=auto — blocking races the page")
		}
		if len(problems) > 0 {
			return report.New("S1", false, strings.Join(problems, "; "))
		}
		return report.New("S1", true, fmt.Sprintf("uc.js tag found (blockingmode=%q, %d mentions)", s.attrs["data-blockingmode"], count))
	}
	return report.New("S1", true, fmt.Sprintf("'cookiebot' mentioned %d× but no uc.js tag (loaded elsewhere?)", count))
}

// S2 — GTM presence, informational: records the container ID, never fails.
func s2(raw []byte) report.Check {
	if !bytes.Contains(raw, []byte("googletagmanager.com/gtm.js")) {
		return report.New("S2", true, "no GTM container in source")
	}
	id := "unknown"
	if m := regexp.MustCompile(`GTM-[A-Z0-9]+`).Find(raw); m != nil {
		id = string(m)
	}
	return report.New("S2", true, "GTM container "+id)
}

// S3 — no live embed iframes: a standalone src attribute matching embedHosts.
func s3(iframes []element, cfg *config.Site) report.Check {
	var live []string
	for _, f := range iframes {
		if src := f.attrs["src"]; src != "" && cfg.EmbedRe().MatchString(src) {
			live = append(live, src)
		}
	}
	return report.New("S3", len(live) == 0,
		fmt.Sprintf("live embed iframes: %d %s", len(live), sample(live)))
}

// S4 — gated iframes carry a valid data-cookieconsent value.
func s4(iframes []element) report.Check {
	gated, bad := 0, []string{}
	for _, f := range iframes {
		src, ok := gatedSrc(f.attrs)
		if !ok {
			continue
		}
		gated++
		consent, ok := f.attrs["data-cookieconsent"]
		if !ok {
			// Without data-cookieconsent, uc.js ignores the element entirely: the embed
			// is not gated, it is simply broken — it will never be restored.
			bad = append(bad, src+" (missing data-cookieconsent)")
			continue
		}
		for _, v := range strings.Split(consent, ",") {
			if !validConsentValues[strings.TrimSpace(v)] {
				bad = append(bad, fmt.Sprintf("%s (invalid value %q)", src, v))
			}
		}
	}
	return report.New("S4", len(bad) == 0,
		fmt.Sprintf("%d gated iframes, %d invalid %s", gated, len(bad), sample(bad)))
}

// S5 — external scripts matching embed/tracker hosts must be gated.
//
// Two gating styles are legitimate and both must be accepted:
//   - hand-written / theme filter: src stays, type="text/plain" + data-cookieconsent
//   - Cookiebot WP plugin:         src moves to data-src + data-cookieconsent (no type change)
//
// A script with a live src and no text/plain gate executes, full stop — including
// when a second type="text/plain" was appended after an existing type (the browser
// keeps the first attribute; see the first-wins note in Run).
func s5(scripts []element, cfg *config.Site) report.Check {
	var offenders []string
	for _, s := range scripts {
		liveSrc := s.attrs["src"]
		parked, isParked := gatedSrc(s.attrs)

		src := liveSrc
		if src == "" {
			src = parked
		}
		if src == "" || (!cfg.EmbedRe().MatchString(src) && !cfg.TrackerRe().MatchString(src)) {
			continue
		}

		_, hasConsent := s.attrs["data-cookieconsent"]
		switch {
		case liveSrc == "" && isParked && hasConsent:
			// src parked in data-src: the browser never fetches it. Gated.
		case liveSrc != "" && s.attrs["type"] == "text/plain" && hasConsent:
			// Neutralised MIME type. Gated.
		default:
			offenders = append(offenders, src)
		}
	}
	return report.New("S5", len(offenders) == 0,
		fmt.Sprintf("ungated embed/tracker scripts: %d %s", len(offenders), sample(offenders)))
}

// S6 — hostile inline scripts: script-injectors that are not gated.
func s6(scripts []element) report.Check {
	var offenders []string
	for _, s := range scripts {
		if s.attrs["src"] != "" {
			continue
		}
		typ := s.attrs["type"]
		if typ == "text/plain" || strings.Contains(typ, "json") {
			continue
		}
		if injectorRe.MatchString(s.text) && !inlineWhitelistRe.MatchString(s.text) {
			head := strings.Join(strings.Fields(s.text), " ")
			if len(head) > 80 {
				head = head[:80] + "…"
			}
			offenders = append(offenders, head)
		}
	}
	return report.New("S6", len(offenders) == 0,
		fmt.Sprintf("ungated inline script-injectors: %d %s", len(offenders), sample(offenders)))
}

// S7 — live third-party <img>: hotlinked images can set cookies (katowice.eu case).
func s7(imgs []element, cfg *config.Site) report.Check {
	hosts := map[string]bool{}
	for _, im := range imgs {
		src := im.attrs["src"]
		if !strings.HasPrefix(src, "http") {
			continue
		}
		u, err := url.Parse(src)
		if err != nil {
			continue
		}
		h := u.Hostname()
		if cfg.FirstParty(h) {
			continue
		}
		if re := cfg.ImageAllowRe(); re != nil && re.MatchString(h) {
			continue
		}
		hosts[h] = true
	}
	var list []string
	for h := range hosts {
		list = append(list, h)
	}
	return report.New("S7", len(list) == 0,
		fmt.Sprintf("third-party img hosts: %d %s", len(list), sample(list)))
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
