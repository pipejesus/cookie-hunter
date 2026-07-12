// Package sitemap expands a Yoast SEO sitemap_index.xml into page URLs.
// Yoast layout: the index lists sub-sitemaps (post-sitemap.xml, page-sitemap.xml, …),
// each listing page URLs. Sampling `limit` per sub-sitemap keeps every content
// type represented without scanning thousands of URLs.
package sitemap

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"cookie-hunter/internal/web"
)

// ponytail: regex over <loc> is enough for sitemap XML; a real XML parser if a site ever breaks it
var locRe = regexp.MustCompile(`(?s)<loc>\s*(.*?)\s*</loc>`)

func locs(body []byte) []string {
	var out []string
	for _, m := range locRe.FindAllSubmatch(body, -1) {
		out = append(out, html.UnescapeString(string(m[1])))
	}
	return out
}

// Expand fetches indexURL and returns up to limit page URLs per sub-sitemap.
// A plain urlset (no sub-sitemaps) works too.
func Expand(indexURL, userAgent string, limit int) ([]string, error) {
	body, status, err := web.Get(indexURL, userAgent)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("%s: HTTP %d", indexURL, status)
	}

	var urls []string
	entries := locs(body)
	taken := 0
	for _, e := range entries {
		if !strings.HasSuffix(strings.ToLower(e), ".xml") {
			// plain urlset entry
			if taken < limit {
				urls = append(urls, e)
				taken++
			}
			continue
		}
		sub, status, err := web.Get(e, userAgent)
		if err != nil || status != 200 {
			return nil, fmt.Errorf("sub-sitemap %s: HTTP %d, %v", e, status, err)
		}
		subLocs := locs(sub)
		if len(subLocs) > limit {
			subLocs = subLocs[:limit]
		}
		urls = append(urls, subLocs...)
	}
	return urls, nil
}
