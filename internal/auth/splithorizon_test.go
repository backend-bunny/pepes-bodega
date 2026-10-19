package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIssuerRewriterRoutesPublicIssuerToInternal(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "internal "+r.URL.Path)
	}))
	defer internal.Close()

	client, err := newIssuerClient("https://dex-ab12.dev.example.net", internal.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get("https://dex-ab12.dev.example.net/.well-known/openid-configuration")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if got, want := string(body), "internal /.well-known/openid-configuration"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestIssuerRewriterLeavesOtherHostsAlone(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "other")
	}))
	defer other.Close()

	client, err := newIssuerClient("https://dex.example.net", "http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get(other.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if body, _ := io.ReadAll(resp.Body); string(body) != "other" {
		t.Fatalf("request to unrelated host was rewritten: %q", body)
	}
}
