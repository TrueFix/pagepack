package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestBuiltFrontendRoutes(t *testing.T) {
	s := &Server{frontend: fstest.MapFS{
		"index.html":        &fstest.MapFile{Data: []byte(`<div id="root"></div><script src="/assets/app.js"></script>`)},
		"assets/app.js":     &fstest.MapFile{Data: []byte("console.log('pagepack')")},
		"assets/font.woff2": &fstest.MapFile{Data: []byte("font-data")},
		"mark.svg":          &fstest.MapFile{Data: []byte("<svg></svg>")},
	}}
	for _, tc := range []struct {
		path, content string
		status        int
	}{
		{"/", `id="root"`, 200},
		{"/assets/app.js", "pagepack", 200},
		{"/assets/font.woff2", "font-data", 200},
		{"/mark.svg", "<svg>", 200},
		{"/.env", "", 404},
		{"/assets/missing.js", "", 404},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.Router().ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.content) {
				t.Fatalf("response: %d %s", w.Code, w.Body.String())
			}
			if tc.path == "/" && w.Header().Get("Cache-Control") != "no-cache" {
				t.Fatal("index must revalidate to pick up new build assets")
			}
		})
	}
}
