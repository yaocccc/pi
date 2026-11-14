package web

import (
	"bytes"
	"embed"
	"net/http"
	"time"
)

//go:embed assets/index.html assets/app.js assets/style.css assets/vendor/xterm.js assets/vendor/xterm.css assets/vendor/addon-fit.js assets/vendor/xterm.LICENSE assets/vendor/addon-fit.LICENSE assets/vendor/addon-unicode-graphemes.js assets/vendor/addon-unicode-graphemes.LICENSE assets/fonts/*
var assets embed.FS

func frontendHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name, contentType := r.URL.Path, ""
		switch name {
		case "/":
			name, contentType = "/index.html", "text/html; charset=utf-8"
		case "/app.js", "/vendor/xterm.js", "/vendor/addon-fit.js", "/vendor/addon-unicode-graphemes.js":
			contentType = "text/javascript; charset=utf-8"
		case "/style.css", "/vendor/xterm.css":
			contentType = "text/css; charset=utf-8"
		case "/fonts/jetbrains-mono-regular.woff2", "/fonts/jetbrains-mono-bold.woff2", "/fonts/jetbrains-mono-italic.woff2", "/fonts/jetbrains-mono-bold-italic.woff2", "/fonts/wqy-microhei-mono.woff2":
			contentType = "font/woff2"
		case "/vendor/xterm.LICENSE", "/vendor/addon-fit.LICENSE", "/vendor/addon-unicode-graphemes.LICENSE", "/fonts/JetBrainsMono-OFL.txt", "/fonts/NerdFonts-LICENSE.txt", "/fonts/WenQuanYi-Apache-2.0.txt", "/fonts/README.md":
			contentType = "text/plain; charset=utf-8"
		default:
			http.NotFound(w, r)
			return
		}
		data, err := assets.ReadFile("assets" + name)
		if err != nil {
			http.Error(w, "asset unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", contentType)
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
	})
}
