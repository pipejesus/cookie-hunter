package config

import "testing"

func TestDetectPluginInstall(t *testing.T) {
	html := []byte(`<script src="https://consent.cookiebot.eu/uc.js" data-cbid="1b56aeb3-f6c3-4753-bea8-ff421ee2f1c2" data-blockingmode="auto"></script>`)
	s := Detect(html, "https://www.komforta.example/about/")
	if s.Domain != "komforta.example" {
		t.Errorf("domain = %q", s.Domain)
	}
	if s.CBID != "1b56aeb3-f6c3-4753-bea8-ff421ee2f1c2" {
		t.Errorf("cbid = %q", s.CBID)
	}
	if s.Region != "eu" {
		t.Errorf("region = %q", s.Region)
	}
}

func TestDetectGTMInstall(t *testing.T) {
	// GTM-loaded Cookiebot: cbid only appears as a uc.js query param, if at all
	html := []byte(`<script>/* gtm */</script><script src="https://consent.cookiebot.com/uc.js?cbid=523d03d1-bf22-4979-9326-cbe11a5ae466&implementation=gtm"></script>`)
	s := Detect(html, "https://tvs.example/")
	if s.CBID != "523d03d1-bf22-4979-9326-cbe11a5ae466" {
		t.Errorf("cbid = %q", s.CBID)
	}
	if s.Region != "com" {
		t.Errorf("region = %q", s.Region)
	}
}

func TestDetectNothing(t *testing.T) {
	s := Detect([]byte("<html><body>plain</body></html>"), "https://bare.example/")
	if s.CBID != "" || s.Region != "com" || s.Domain != "bare.example" {
		t.Errorf("bare site config = %+v", s)
	}
}
