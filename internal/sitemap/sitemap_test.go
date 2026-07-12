package sitemap

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExpandYoastIndex(t *testing.T) {
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/sitemap_index.xml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<sitemapindex>
			<sitemap><loc>%s/post-sitemap.xml</loc></sitemap>
			<sitemap><loc>%s/page-sitemap.xml</loc></sitemap>
		</sitemapindex>`, srv.URL, srv.URL)
	})
	mux.HandleFunc("/post-sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<urlset>
			<url><loc>https://x.example/post-1/</loc></url>
			<url><loc>https://x.example/post-2/</loc></url>
			<url><loc>https://x.example/post-3/</loc></url>
		</urlset>`)
	})
	mux.HandleFunc("/page-sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<urlset><url><loc>https://x.example/about/?a=1&amp;b=2</loc></url></urlset>`)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	urls, err := Expand(srv.URL+"/sitemap_index.xml", "test-agent", 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"https://x.example/post-1/",
		"https://x.example/post-2/", // limit 2 per sub-sitemap: post-3 dropped
		"https://x.example/about/?a=1&b=2",
	}
	if len(urls) != len(want) {
		t.Fatalf("got %d urls %v, want %d", len(urls), urls, len(want))
	}
	for i := range want {
		if urls[i] != want[i] {
			t.Errorf("urls[%d] = %q, want %q", i, urls[i], want[i])
		}
	}
}
