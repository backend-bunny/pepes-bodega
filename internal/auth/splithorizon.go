package auth

import (
	"fmt"
	"net/http"
	"net/url"
)

// issuerRewriter sends requests aimed at the public OIDC issuer to an
// internal URL instead. The browser reaches the IdP through a public URL
// (e.g. a NetBird-exposed https endpoint) that pods cannot hairpin through,
// while ID tokens must still carry the public issuer. Discovery, token
// exchange and JWKS fetches all go through this transport.
type issuerRewriter struct {
	public, internal *url.URL
	next             http.RoundTripper
}

func newIssuerClient(publicIssuer, internalURL string) (*http.Client, error) {
	pub, err := url.Parse(publicIssuer)
	if err != nil {
		return nil, fmt.Errorf("parse issuer: %w", err)
	}
	in, err := url.Parse(internalURL)
	if err != nil || in.Scheme == "" || in.Host == "" {
		return nil, fmt.Errorf("invalid internal OIDC URL %q", internalURL)
	}
	return &http.Client{Transport: &issuerRewriter{public: pub, internal: in, next: http.DefaultTransport}}, nil
}

func (t *issuerRewriter) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != t.public.Host || req.URL.Scheme != t.public.Scheme {
		return t.next.RoundTrip(req)
	}
	r := req.Clone(req.Context())
	r.URL.Scheme = t.internal.Scheme
	r.URL.Host = t.internal.Host
	r.Host = ""
	return t.next.RoundTrip(r)
}
