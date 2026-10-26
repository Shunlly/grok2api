package httpserver

import (
	"bytes"
	"compress/gzip"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 静态前端资源压缩：优先使用构建期生成的 .br/.gz 文件，否则对可压缩类型即时 gzip 并缓存。
const frontendCompressMinBytes = 1024

var compressibleFrontendTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json",
	".svg":         "image/svg+xml",
	".txt":         "text/plain; charset=utf-8",
	".map":         "application/json",
	".webmanifest": "application/manifest+json",
}

type gzipCacheEntry struct {
	modTime time.Time
	size    int64
	data    []byte
}

var frontendGzipCache sync.Map // path -> gzipCacheEntry

func frontendContentType(name string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(name))
	if t, ok := compressibleFrontendTypes[ext]; ok {
		return t, true
	}
	return mime.TypeByExtension(ext), false
}

// acceptsEncoding 解析 Accept-Encoding，q=0 视为拒绝。
func acceptsEncoding(header, encoding string) bool {
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		name := strings.ToLower(strings.TrimSpace(fields[0]))
		if name != encoding && name != "*" {
			continue
		}
		q := 1.0
		for _, f := range fields[1:] {
			f = strings.TrimSpace(f)
			if strings.HasPrefix(f, "q=") {
				if v, err := strconv.ParseFloat(f[2:], 64); err == nil {
					q = v
				}
			}
		}
		if name == encoding || q > 0 {
			return q > 0
		}
	}
	return false
}

func serveCompressedFrontendFile(w http.ResponseWriter, r *http.Request, fullPath string) bool {
	contentType, compressible := frontendContentType(fullPath)
	if !compressible {
		return false
	}
	w.Header().Add("Vary", "Accept-Encoding")
	accept := r.Header.Get("Accept-Encoding")
	if accept == "" || r.Header.Get("Range") != "" {
		return false
	}
	orig, err := os.Stat(fullPath)
	if err != nil {
		return false
	}
	for _, enc := range []struct{ name, ext string }{{"br", ".br"}, {"gzip", ".gz"}} {
		if !acceptsEncoding(accept, enc.name) {
			continue
		}
		f, err := os.Open(fullPath + enc.ext)
		if err != nil {
			continue
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			f.Close()
			continue
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Encoding", enc.name)
		http.ServeContent(w, r, "", orig.ModTime(), f)
		f.Close()
		return true
	}
	if !acceptsEncoding(accept, "gzip") || orig.Size() < frontendCompressMinBytes {
		return false
	}
	data, ok := gzipFrontendFile(fullPath, orig)
	if !ok {
		return false
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Encoding", "gzip")
	http.ServeContent(w, r, "", orig.ModTime(), bytes.NewReader(data))
	return true
}

func gzipFrontendFile(fullPath string, info os.FileInfo) ([]byte, bool) {
	if v, ok := frontendGzipCache.Load(fullPath); ok {
		e := v.(gzipCacheEntry)
		if e.modTime.Equal(info.ModTime()) && e.size == info.Size() {
			return e.data, true
		}
	}
	raw, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, false
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if _, err := zw.Write(raw); err != nil || zw.Close() != nil {
		return nil, false
	}
	frontendGzipCache.Store(fullPath, gzipCacheEntry{modTime: info.ModTime(), size: info.Size(), data: buf.Bytes()})
	return buf.Bytes(), true
}
