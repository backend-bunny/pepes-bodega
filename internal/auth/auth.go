// Package auth authenticates requests: browser sessions established through
// an OIDC authorization-code flow, and bearer API tokens for the CLI.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/backend-bunny/pepes-bodega/internal/config"
	"github.com/backend-bunny/pepes-bodega/internal/db"
)

const (
	sessionCookie = "bodega_session"
	stateCookie   = "bodega_oidc_state"
	// TokenPrefix marks API tokens so they are recognisable in configs and leaks.
	TokenPrefix = "pbt_"
)

type ctxKey struct{}

// UserFrom returns the authenticated user stored in the request context.
func UserFrom(ctx context.Context) (db.User, bool) {
	u, ok := ctx.Value(ctxKey{}).(db.User)
	return u, ok
}

func WithUser(ctx context.Context, u db.User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

type Authenticator struct {
	q          *db.Queries
	log        *slog.Logger
	sessionTTL time.Duration
	secure     bool
	publicURL  string
	identityNS string
	verifier   *oidc.IDTokenVerifier
	oauth      *oauth2.Config
	devUser    string
	// httpClient routes server-side IdP calls (nil = default client).
	httpClient *http.Client
}

func New(ctx context.Context, cfg config.API, q *db.Queries, log *slog.Logger) (*Authenticator, error) {
	var err error
	a := &Authenticator{
		q:          q,
		log:        log,
		sessionTTL: cfg.SessionTTL,
		secure:     !cfg.InsecureCookies,
		publicURL:  cfg.PublicURL,
		devUser:    cfg.DevAuthUser,
	}
	if a.devUser != "" {
		log.Warn("DEV AUTH ENABLED: every request is authenticated", "user", a.devUser)
		return a, nil
	}
	if cfg.OIDC.InternalURL != "" {
		if a.httpClient, err = newIssuerClient(cfg.OIDC.IssuerURL, cfg.OIDC.InternalURL); err != nil {
			return nil, err
		}
		log.Info("OIDC split horizon", "issuer", cfg.OIDC.IssuerURL, "internal", cfg.OIDC.InternalURL)
	}
	provider, err := oidc.NewProvider(a.idpContext(ctx), cfg.OIDC.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery for %s: %w", cfg.OIDC.IssuerURL, err)
	}
	a.identityNS = cfg.OIDC.IdentityNamespace
	a.verifier = provider.Verifier(&oidc.Config{ClientID: cfg.OIDC.ClientID})
	a.oauth = &oauth2.Config{
		ClientID:     cfg.OIDC.ClientID,
		ClientSecret: cfg.OIDC.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  cfg.PublicURL + "/auth/callback",
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
	return a, nil
}

// HashSecret hashes a session id or API token for storage. Secrets are 256-bit
// random values, so an unsalted SHA-256 is sufficient.
func HashSecret(secret string) []byte {
	h := sha256.Sum256([]byte(secret))
	return h[:]
}

func randomSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// NewAPIToken returns a fresh plaintext token (shown once) and its hash.
func NewAPIToken() (plain string, hash []byte) {
	plain = TokenPrefix + randomSecret()
	return plain, HashSecret(plain)
}

// Middleware authenticates via bearer token or session cookie and rejects
// anonymous requests with 401.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := a.authenticate(r)
		if err != nil {
			if !errors.Is(err, db.ErrNotFound) {
				a.log.Error("authenticate", "err", err)
			}
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), u)))
	})
}

func (a *Authenticator) authenticate(r *http.Request) (db.User, error) {
	if a.devUser != "" {
		return a.q.UpsertUser(r.Context(), "dev", a.devUser, a.devUser+"@dev.local", a.devUser)
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return a.q.TokenUser(r.Context(), HashSecret(strings.TrimPrefix(h, "Bearer ")))
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return db.User{}, db.ErrNotFound
	}
	return a.q.SessionUser(r.Context(), HashSecret(c.Value))
}

func (a *Authenticator) cookie(name, value string, maxAge time.Duration) *http.Cookie {
	return &http.Cookie{
		Name: name, Value: value, Path: "/", MaxAge: int(maxAge.Seconds()),
		HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode,
	}
}

// Login starts the OIDC authorization-code flow.
func (a *Authenticator) Login(w http.ResponseWriter, r *http.Request) {
	if a.devUser != "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	state := randomSecret()
	verifier := oauth2.GenerateVerifier()
	http.SetCookie(w, a.cookie(stateCookie, state+"."+verifier, 10*time.Minute))
	url := a.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, url, http.StatusFound)
}

// Callback completes the OIDC flow and creates a session.
func (a *Authenticator) Callback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(stateCookie)
	state, verifier, ok := strings.Cut(valueOr(c, err), ".")
	if !ok || r.URL.Query().Get("state") != state {
		http.Error(w, "invalid oauth state", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, a.cookie(stateCookie, "", -1))

	tok, err := a.oauth.Exchange(a.idpContext(r.Context()), r.URL.Query().Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		a.log.Warn("oidc exchange", "err", err)
		http.Error(w, "login failed", http.StatusUnauthorized)
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	idt, err := a.verifier.Verify(r.Context(), rawID)
	if err != nil {
		a.log.Warn("oidc verify", "err", err)
		http.Error(w, "login failed", http.StatusUnauthorized)
		return
	}
	var claims struct {
		Email             string `json:"email"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	_ = idt.Claims(&claims)
	name := claims.Name
	if name == "" {
		name = claims.PreferredUsername
	}
	u, err := a.q.UpsertUser(r.Context(), a.identityNS, idt.Subject, claims.Email, name)
	if err != nil {
		a.log.Error("upsert user", "err", err)
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}
	sid := randomSecret()
	if err := a.q.CreateSession(r.Context(), HashSecret(sid), u.ID, time.Now().Add(a.sessionTTL)); err != nil {
		a.log.Error("create session", "err", err)
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, a.cookie(sessionCookie, sid, a.sessionTTL))
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *Authenticator) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = a.q.DeleteSession(r.Context(), HashSecret(c.Value))
	}
	http.SetCookie(w, a.cookie(sessionCookie, "", -1))
	w.WriteHeader(http.StatusNoContent)
}

// idpContext makes go-oidc and oauth2 use the split-horizon client, if any.
func (a *Authenticator) idpContext(ctx context.Context) context.Context {
	if a.httpClient == nil {
		return ctx
	}
	return oidc.ClientContext(ctx, a.httpClient)
}

func valueOr(c *http.Cookie, err error) string {
	if err != nil {
		return ""
	}
	return c.Value
}
