package httpserver

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupCompressFrontend(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	big := strings.Repeat("console.log('hello world');\n", 200)
	os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>"+strings.Repeat("x", 2000)+"</html>"), 0o644)
	os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte(big), 0o644)
	os.WriteFile(filepath.Join(dir, "assets", "app.js.br"), []byte("BROTLI"), 0o644)
	os.WriteFile(filepath.Join(dir, "assets", "style.css"), []byte(strings.Repeat("a{b:c}", 500)), 0o644)
	os.WriteFile(filepath.Join(dir, "assets", "img.png"), bytes.Repeat([]byte{1}, 5000), 0o644)
	r := gin.New()
	r.GET("/v1/stream", func(c *gin.Context) { c.String(200, strings.Repeat("data: x\n\n", 500)) })
	registerFrontend(r, dir)
	return r, dir
}

func doReq(r http.Handler, path, accept string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if accept != "" {
		req.Header.Set("Accept-Encoding", accept)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestFrontendServesPrecompressedBrotli(t *testing.T) {
	r, _ := setupCompressFrontend(t)
	w := doReq(r, "/assets/app.js", "gzip, br")
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "br" || w.Body.String() != "BROTLI" {
		t.Fatalf("code=%d enc=%q body=%q", w.Code, w.Header().Get("Content-Encoding"), w.Body.String())
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("content-type %q", w.Header().Get("Content-Type"))
	}
	if w.Header().Get("Vary") != "Accept-Encoding" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("headers %v", w.Header())
	}
}

func TestFrontendOnTheFlyGzip(t *testing.T) {
	r, dir := setupCompressFrontend(t)
	for _, p := range []string{"/assets/app.js", "/assets/style.css", "/", "/dashboard"} {
		w := doReq(r, p, "gzip")
		if w.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("%s: enc=%q", p, w.Header().Get("Content-Encoding"))
		}
		zr, err := gzip.NewReader(w.Body)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(zr)
		name := "index.html"
		if strings.HasPrefix(p, "/assets/") {
			name = strings.TrimPrefix(p, "/")
		}
		want, _ := os.ReadFile(filepath.Join(dir, name))
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: body mismatch", p)
		}
	}
}

func TestFrontendNoCompressionCases(t *testing.T) {
	r, _ := setupCompressFrontend(t)
	if w := doReq(r, "/assets/app.js", ""); w.Header().Get("Content-Encoding") != "" || w.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("identity: %v", w.Header())
	}
	if w := doReq(r, "/assets/app.js", "gzip;q=0, br;q=0"); w.Header().Get("Content-Encoding") != "" {
		t.Fatalf("q=0 should disable")
	}
	if w := doReq(r, "/assets/img.png", "gzip, br"); w.Header().Get("Content-Encoding") != "" {
		t.Fatalf("png should not compress")
	}
	if w := doReq(r, "/v1/stream", "gzip, br"); w.Header().Get("Content-Encoding") != "" || w.Header().Get("Vary") != "" {
		t.Fatalf("api must not compress: %v", w.Header())
	}
}
