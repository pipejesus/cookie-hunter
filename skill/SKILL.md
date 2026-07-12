---
name: cookie-hunter
description: Verify that a website blocks cookies/trackers before consent using the cookie-hunter CLI (Cookiebot CMP sites). Use when asked to check cookie compliance, GDPR/ePrivacy prior-consent blocking, Cookiebot setup, pre-consent tracker leakage, or to compare a site before/after a consent-related deploy.
---

# Cookie Hunter — agent guide

`cookie-hunter` scans sites with real headless Chrome and reports whether trackers are
actually blocked before consent. You interpret its findings; it gathers the evidence.

## Availability

`cookie-hunter` must be on PATH (check with `cookie-hunter 2>&1 | head -1`). If missing:
download a static binary from https://github.com/pipejesus/cookie-hunter/releases
(linux/mac/windows, no dependencies), or `go build` from the repo root. Phases 2–3
additionally need Chrome/Chromium installed; without it use `-static-only`.

## Workflow

1. **Generate a site config** (once per site):
   `cookie-hunter init https://example.com` → writes `example.com.json` with domain,
   cbid, region (auto-detected from page source) and a seeded cookie allowlist.
   - cbid NOT FOUND usually means Cookiebot is loaded via GTM (or absent) — checklist
     checks will be skipped until the user provides the cbid.
   - The config contains the cbid, which identifies the site's Cookiebot account:
     keep it out of git repos and don't quote it in shared output.
2. **Scan:**
   - Single page: `cookie-hunter scan -config example.com.json https://example.com/page/`
   - Many pages cheaply: add `-static-only` (no browser, safe for hundreds of URLs)
   - WordPress/Yoast: `cookie-hunter scan -config example.com.json -sitemap https://example.com/sitemap_index.xml -limit 5`
   - Flags go BEFORE the URLs (Go stdlib flag parsing).
3. **For your own analysis add `-json`** — structured checks with observed values.
   Humans prefer the default matrix output.
4. Every run archives evidence in `snapshots/<domain>/<timestamp>/`: raw HTML,
   `configuration.js`, `*.runtime.json` (all network requests + cookie jar),
   `results.json`. Read these when a verdict needs explaining. Diffing two run
   directories is the before/after report.
5. Exit code: 0 = no FAILs, 1 = at least one FAIL, 2 = usage/config error. WARN and
   SKIP never affect the exit code — usable directly in CI.

## Check reference

| ID | Meaning | On FAIL, tell the user |
|---|---|---|
| S1 | Cookiebot tag present, sync, right region | WARN "GTM-only": banner loads late, no auto-block possible — recommend in-`<head>` install (WP plugin). FAIL async/wrong region: blocking races the page. |
| S2 | GTM container (informational, never fails) | — |
| S3 | Live (ungated) embed iframes in HTML | These embeds set cookies pre-consent; gate them (data-cookieblock-src) |
| S4 | Gated iframes have valid data-cookieconsent | Fix the attribute values (preferences/statistics/marketing/ignore) |
| S5 | External embed/tracker scripts not text/plain-gated | Gate them or let auto-block handle them |
| S6 | Ungated inline scripts that inject other scripts | INSPECT before reporting: benign injectors exist (WP emoji loader, picture polyfill, JSON-LD injectors). Hostile ones assemble tracker URLs char-by-char (never grep vendor names — that's why detection is behavioral). Judge each offender from the snapshot HTML. |
| S7 | Third-party `<img>` hosts | Hotlinked images can set cookies; verify each host, allowlist CDNs in config |
| C1 | Auto-block checklist exists (404 = never scanned) | Trigger a scan in Cookiebot admin |
| C2 | Known trackers marked cat 1/5 (= NOT blocked) | Reclassify those cookies/tags in Cookiebot admin — auto-block only blocks cat 2–4 |
| C3 | First-party essentials marked blockable (cat 2–4) | Reclassify to Necessary BEFORE enabling auto-block, or the site breaks |
| C4 | Checklist drift vs previous snapshot (informational) | — |
| R1 | Tracker requests pre-consent | Real leakage — identify the source (GTM tag? theme? content?). Requests carrying `gcs=G100` are Consent Mode denied pings (cookieless, intentional) and are already excluded — do not report those as leakage. |
| R2 | GA hits carry `gcs=G100` pre-consent | Google Consent Mode missing/broken — fix Consent Mode defaults before anything else |
| R3 | CMP script loaded at all | CMP absent or blocked — nothing else can work |
| D1 | No consent recorded pre-interaction | A consent is being auto-submitted — find what calls the Cookiebot API |
| D2 | Banner visible | Banner suppressed/broken — consent can't be "prior" if never asked |
| D3 | Zero live embed iframes at runtime | Same as S3 but runtime truth (JS-inserted embeds) |
| K1 | Only allowlisted cookies pre-consent | For UNKNOWN cookies: investigate what sets them. Session-essential → add to config allowlist (human decision). Tracking (`_ga`, `_fbp`, third-party domains) → real violation. |
| A1–A4 | Post-consent restore (only with `-consent`) | Blocked-after-consent = broken UX; check data-cookieblock-src restore |

## Hard rules

- **Never run `-consent` against a production site without the user's explicit OK** —
  it submits a real consent, written into the client's Cookiebot consent log.
- Judge *blocking* by DOM (S3/S4/D3), *leakage* by network (R1) — lazy-loading makes
  the counts legitimately differ; that alone is not a finding.
- If static and runtime results disagree, suspect a stale page cache: refetch with a
  cache-busting query param and compare.
- One green page proves that page only. For site-wide claims scan a sitemap sample
  (at minimum: homepage + one page per embed type + one article with third-party content).
- The Cookiebot monthly scan report is not a test oracle — it lags deploys and samples
  unpredictably. Trust cookie-hunter's direct observation.
