package runtime

import "testing"

// A library download is not a measurement hit, and only a measurement hit can
// carry a consent signal. kurowski.pl (2026-09-21) read "1 of 2 GA hits without
// gcs=G100" because `analytics.js` — a script file — was scored as a hit; the
// site's only real hit carried gcs=G100 and its Google tags were behaving.
func TestGAMeasurementVsLibrary(t *testing.T) {
	hits := []string{
		"https://region1.google-analytics.com/g/collect?v=2&tid=G-X&gcs=G100",
		"https://www.google-analytics.com/collect?v=1&tid=UA-1-1",
		"https://region1.analytics.google.com/g/collect?v=2&tid=G-X",
		"https://region1.analytics.google.com/measurement/conversion?en=page_view",
		"https://www.google-analytics.com/j/collect?v=1",
	}
	libs := []string{
		"https://www.google-analytics.com/analytics.js",
		"https://www.google-analytics.com/ga.js",
		"https://www.googletagmanager.com/gtag/js?id=UA-41248465-1",
		"https://www.googletagmanager.com/gtm.js?id=GTM-ABC",
		"https://connect.facebook.net/en_US/fbevents.js",
	}
	for _, u := range hits {
		if !gaMeasurementRe.MatchString(u) {
			t.Errorf("measurement hit not recognised: %s", u)
		}
	}
	for _, u := range libs {
		if gaMeasurementRe.MatchString(u) {
			t.Errorf("library file scored as a measurement hit: %s", u)
		}
		if !tagLoaderRe.MatchString(u) {
			t.Errorf("library file not recognised as a tag loader: %s", u)
		}
	}
	// A loader must never swallow a real hit.
	for _, u := range hits {
		if tagLoaderRe.MatchString(u) {
			t.Errorf("measurement hit misread as a tag loader: %s", u)
		}
	}
}
