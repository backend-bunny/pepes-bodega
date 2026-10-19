// Command devproxy is a dev-only HTTP reverse proxy used by hack/dev-netbird.sh
// for the Tilt UI. The NetBird proxy forwards requests with a Host header other
// than the public hostname, and Tilt rejects any request whose Origin host
// differs from Host ("cross-origin request forbidden"), which breaks its UI
// websocket. devproxy restores Host to the public name, so Tilt's origin check
// still only accepts pages served from the exposed URL.
package main

import (
	"flag"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func main() {
	listen := flag.String("listen", "", "address to listen on, e.g. 100.84.0.1:10350")
	target := flag.String("target", "", "upstream base URL, e.g. http://127.0.0.1:10350")
	host := flag.String("host", "", "Host header to send upstream (default: keep the incoming one)")
	flag.Parse()
	if *listen == "" || *target == "" {
		log.Fatal("-listen and -target are required")
	}
	up, err := url.Parse(*target)
	if err != nil {
		log.Fatalf("parse -target: %v", err)
	}
	proxy := &httputil.ReverseProxy{
		ModifyResponse: func(resp *http.Response) error {
			if strings.EqualFold(resp.Request.Header.Get("Upgrade"), "websocket") {
				log.Printf("websocket %s: upstream answered %s", resp.Request.URL.Path, resp.Status)
			}
			return nil
		},
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(up)
			r.Out.Host = r.In.Host
			if *host != "" {
				r.Out.Host = *host
			}
			if strings.EqualFold(r.In.Header.Get("Upgrade"), "websocket") {
				log.Printf("websocket %s: incoming Host=%q Origin=%q -> upstream Host=%q",
					r.In.URL.Path, r.In.Host, r.In.Header.Get("Origin"), r.Out.Host)
			}
		},
	}
	log.Printf("devproxy %s -> %s (Host: %s)", *listen, up, valueOr(*host, "unchanged"))
	log.Fatal(http.ListenAndServe(*listen, proxy))
}

func valueOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
