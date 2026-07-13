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
| S3 | Live (ungated) embed iframes in HTML | These embeds set cookies pre-consent; gate them (`data-cookieblock-src` or `data-src` — uc.js restores both) |
| S4 | Gated iframes have valid data-cookieconsent | Fix the attribute values (preferences/statistics/marketing/ignore). Missing `data-cookieconsent` = not gated but BROKEN: uc.js never restores it. |
| S5 | External embed/tracker scripts not gated | Two valid styles: `type="text/plain"` + `data-cookieconsent` (keeps src), or src parked in `data-src` + `data-cookieconsent` (what the Cookiebot WP plugin emits). **Watch for a duplicate `type`:** appending `type="text/plain"` to a tag that already has `type="text/javascript"` is a no-op — the browser keeps the FIRST attribute and the script still runs. |
| S6 | Ungated inline scripts that inject other scripts | INSPECT before reporting: benign injectors exist (WP emoji loader, picture polyfill, JSON-LD injectors). Hostile ones assemble tracker URLs char-by-char (never grep vendor names — that's why detection is behavioral). Judge each offender from the snapshot HTML. |
| S7 | Third-party `<img>` hosts | Hotlinked images can set cookies; verify each host, allowlist CDNs in config |
| C1 | Auto-block checklist exists (404 = never scanned) | Trigger a scan in Cookiebot admin |
| C2 | Trackers, or ANY third-party entry, marked cat 1/5 (= NOT blocked) | Auto-block only blocks cat 2–4; cat 1 (Necessary) and cat 5 (Unclassified) both mean "don't block". **The checklist is DERIVED, not editable** — there is no dropdown for a script URL. Fix the *cookies that script sets* in the cookie list, then rescan; Cookiebot recomputes the tag's category. |
| C3 | First-party essentials marked blockable (cat 2–4) | Reclassify to Necessary and rescan BEFORE enabling auto-block, or the site breaks. **But the entry is keyed on the exact URL incl. `?ver=`, so this detaches on the next plugin update** — for anything version-stamped, the durable fix is `data-cookieconsent="ignore"` on the tag (uc.js skips it) rather than a category in the panel. |
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
- **The static checks can only see hosts in `embedHosts`/`trackerHosts`.** A green S3/S5
  means "nothing I recognise", not "nothing there". A site's own video platform
  (`video.onnetwork.tv` on tvs.pl) or a raw Maps iframe will pass S3/S5 while leaking —
  R1/K1 are what caught both. If R1/K1 fail while S3/S5 pass, the host list is the bug:
  add the vendor to the config and rescan.
- **Name registrable domains in `embedHosts`, not subdomains.** `open\.spotify\.com`
  silently misses `creators.spotify.com`; `player\.vimeo\.com` misses `vimeo.com`.
- If static and runtime results disagree, suspect a stale page cache: refetch with a
  cache-busting query param and compare.
- One green page proves that page only. For site-wide claims scan a sitemap sample
  (at minimum: homepage + one page per embed type + one article with third-party content).
- The Cookiebot monthly scan report is not a test oracle — it lags deploys and samples
  unpredictably. Trust cookie-hunter's direct observation.
