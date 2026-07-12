// Cookie Hunter — verifies that a Cookiebot-equipped site blocks trackers
// before consent. Check catalog: ../claude-check-process.md.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"cookie-hunter/internal/checklist"
	"cookie-hunter/internal/config"
	"cookie-hunter/internal/report"
	"cookie-hunter/internal/runtime"
	"cookie-hunter/internal/sitemap"
	"cookie-hunter/internal/static"
	"cookie-hunter/internal/web"
)

const (
	staticWorkers  = 10
	browserWorkers = 3
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "scan" {
		fmt.Fprintln(os.Stderr, "usage: cookie-hunter scan [flags] <url>...   (see -h)")
		os.Exit(2)
	}

	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	configPath := fs.String("config", "", "site config JSON (domain, cbid, region, trackerHosts, …)")
	sitemapURL := fs.String("sitemap", "", "Yoast sitemap_index.xml URL — expands sub-sitemaps")
	limit := fs.Int("limit", 10, "URLs sampled per sub-sitemap")
	staticOnly := fs.Bool("static-only", false, "phases 0–1 only, no browser")
	consent := fs.Bool("consent", false, "phase 3: submit REAL consent (logged in the Cookiebot account!)")
	headed := fs.Bool("headed", false, "show the Chrome window instead of running headless")
	jsonOut := fs.Bool("json", false, "machine-readable output")
	outDir := fs.String("out", "", "snapshot dir (default snapshots/<domain>/<timestamp>)")
	fs.Parse(os.Args[2:])
	runtime.Headed = *headed

	cfg, err := config.Load(*configPath)
	fatal(err)

	urls := fs.Args()
	if *sitemapURL != "" {
		fromSitemap, err := sitemap.Expand(*sitemapURL, cfg.UserAgent, *limit)
		fatal(err)
		urls = append(urls, fromSitemap...)
	}
	if len(urls) == 0 {
		fmt.Fprintln(os.Stderr, "no URLs: pass them as arguments or via -sitemap")
		os.Exit(2)
	}
	cfg.DeriveDomain(urls[0])

	run := &report.Run{
		Domain:    cfg.Domain,
		StartedAt: time.Now().Format("2006-01-02 15:04:05"),
	}
	if *outDir == "" {
		*outDir = filepath.Join("snapshots", cfg.Domain, time.Now().Format("20060102-150405"))
	}
	fatal(os.MkdirAll(*outDir, 0o755))
	run.SnapshotDir = *outDir

	// Phase 1 — checklist (once per domain)
	if cfg.CBID == "" {
		run.Checklist = []report.Check{report.Skipped("C1", "no cbid configured — checklist checks skipped")}
	} else {
		raw, status, err := checklist.Fetch(cfg)
		if err != nil {
			run.Checklist = []report.Check{report.Skipped("C1", "checklist fetch failed: " + err.Error())}
		} else {
			if status == 200 {
				fatal(os.WriteFile(filepath.Join(*outDir, "configuration.js"), raw, 0o644))
			}
			run.Checklist = checklist.Run(raw, status, cfg, previousChecklist(*outDir))
		}
	}

	// Phase 0 — static, then phases 2–3 — runtime
	run.URLs = make([]report.URLReport, len(urls))
	pool(len(urls), staticWorkers, func(i int) {
		run.URLs[i] = scanStatic(cfg, urls[i], *outDir)
	})
	if !*staticOnly {
		pool(len(urls), browserWorkers, func(i int) {
			if run.URLs[i].Error != "" {
				return
			}
			scanRuntime(cfg, urls[i], *outDir, *consent, &run.URLs[i])
		})
	}

	fatal(writeJSONFile(filepath.Join(*outDir, "results.json"), run))
	if *jsonOut {
		fatal(run.WriteJSON(os.Stdout))
	} else {
		run.WriteHuman(os.Stdout)
	}
	if run.HasFailures() {
		os.Exit(1)
	}
}

func scanStatic(cfg *config.Site, url, outDir string) report.URLReport {
	rep := report.URLReport{URL: url}
	raw, status, err := web.Get(url, cfg.UserAgent)
	if err != nil {
		rep.Error = err.Error()
		return rep
	}
	if status != 200 {
		rep.Error = fmt.Sprintf("HTTP %d", status)
		return rep
	}
	os.WriteFile(filepath.Join(outDir, safeName(url)+".html"), raw, 0o644)
	rep.Checks = static.Run(raw, url, cfg)
	return rep
}

func scanRuntime(cfg *config.Site, url, outDir string, consent bool, rep *report.URLReport) {
	res, err := runtime.Scan(context.Background(), cfg, url, consent)
	if err != nil {
		rep.Checks = append(rep.Checks, report.Check{ID: "R0", Status: report.Fail, Detail: "browser run failed: " + err.Error()})
		return
	}
	writeJSONFile(filepath.Join(outDir, safeName(url)+".runtime.json"), res)
	rep.Checks = append(rep.Checks, res.Checks...)
}

// previousChecklist finds the newest configuration.js among earlier runs of
// the same domain, for the C4 drift check.
func previousChecklist(outDir string) []byte {
	pattern := filepath.Join(filepath.Dir(outDir), "*", "configuration.js")
	matches, _ := filepath.Glob(pattern)
	sort.Strings(matches)
	for i := len(matches) - 1; i >= 0; i-- {
		if filepath.Dir(matches[i]) == outDir {
			continue
		}
		if raw, err := os.ReadFile(matches[i]); err == nil {
			return raw
		}
	}
	return nil
}

func pool(n, workers int, fn func(i int)) {
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}

var unsafeChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func safeName(url string) string {
	name := unsafeChars.ReplaceAllString(url, "-")
	if len(name) > 120 {
		name = name[:120]
	}
	return name
}

func writeJSONFile(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "cookie-hunter:", err)
		os.Exit(2)
	}
}
