package server

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"time"
)

var zeroTime time.Time

//go:embed all:dist
var distFS embed.FS

// NewStaticHandler 提供嵌入的前端文件。找不到的路径回退到 index.html，由前端路由处理。
func NewStaticHandler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return staticHandler{fsys: sub}
}

type staticHandler struct{ fsys fs.FS }

func (h staticHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	name := strings.TrimPrefix(path.Clean("/"+req.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	data, err := fs.ReadFile(h.fsys, name)
	if err != nil {
		name = "index.html"
		if data, err = fs.ReadFile(h.fsys, name); err != nil {
			http.NotFound(w, req)
			return
		}
	}
	http.ServeContent(w, req, name, zeroTime, bytes.NewReader(data))
}

// NewDevProxy 把请求转发给前端开发服务器（--dev）。
func NewDevProxy(target string) (http.Handler, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	return httputil.NewSingleHostReverseProxy(u), nil
}

// WithFrontend 把 /api 开头的请求交给 api，其余交给 frontend。
func WithFrontend(api, frontend http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if p := req.URL.Path; p == "/api" || strings.HasPrefix(p, "/api/") {
			api.ServeHTTP(w, req)
			return
		}
		frontend.ServeHTTP(w, req)
	})
}
