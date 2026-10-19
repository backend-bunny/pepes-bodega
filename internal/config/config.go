// Package config loads service configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type API struct {
	ListenAddr  string
	DatabaseURL string
	NATSURL     string
	S3          S3
	OIDC        OIDC
	// PublicURL is the externally visible base URL (used for OIDC redirects).
	PublicURL string
	// SessionTTL bounds how long a browser session lives.
	SessionTTL time.Duration
	// MaxUploadBytes caps a single uploaded file.
	MaxUploadBytes int64
	// InsecureCookies disables the Secure cookie flag (plain-http dev only).
	InsecureCookies bool
	// DevAuthUser, when set, bypasses OIDC and logs everyone in as this user.
	// Only for local tests; refused unless InsecureCookies is also set.
	DevAuthUser string
}

type S3 struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

type OIDC struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	// InternalURL, if set, is where the API itself reaches the IdP when the
	// public IssuerURL is not reachable from inside the cluster.
	InternalURL string
	// IdentityNamespace scopes OIDC subjects when keying users. Defaults to the
	// issuer; set it when the issuer URL is not stable (dev: random netbird URLs)
	// so the same IdP user keeps the same account.
	IdentityNamespace string
}

func LoadAPI() (API, error) {
	c := API{
		ListenAddr:      env("BODEGA_LISTEN_ADDR", ":8080"),
		DatabaseURL:     env("BODEGA_DATABASE_URL", ""),
		NATSURL:         env("BODEGA_NATS_URL", "nats://localhost:4222"),
		PublicURL:       strings.TrimRight(env("BODEGA_PUBLIC_URL", "http://localhost:8080"), "/"),
		SessionTTL:      envDuration("BODEGA_SESSION_TTL", 30*24*time.Hour),
		MaxUploadBytes:  envInt("BODEGA_MAX_UPLOAD_BYTES", 2<<30),
		InsecureCookies: envBool("BODEGA_INSECURE_COOKIES", false),
		DevAuthUser:     env("BODEGA_DEV_AUTH_USER", ""),
		S3: S3{
			Endpoint:  env("BODEGA_S3_ENDPOINT", "localhost:8333"),
			Region:    env("BODEGA_S3_REGION", "us-east-1"),
			Bucket:    env("BODEGA_S3_BUCKET", "bodega"),
			AccessKey: env("BODEGA_S3_ACCESS_KEY", ""),
			SecretKey: env("BODEGA_S3_SECRET_KEY", ""),
			UseSSL:    envBool("BODEGA_S3_USE_SSL", false),
		},
		OIDC: OIDC{
			IssuerURL:    env("BODEGA_OIDC_ISSUER", ""),
			ClientID:     env("BODEGA_OIDC_CLIENT_ID", ""),
			ClientSecret: env("BODEGA_OIDC_CLIENT_SECRET", ""),
			InternalURL:  env("BODEGA_OIDC_INTERNAL_URL", ""),
			IdentityNamespace: env("BODEGA_OIDC_IDENTITY_NAMESPACE", ""),
		},
	}
	var missing []string
	if c.DatabaseURL == "" {
		missing = append(missing, "BODEGA_DATABASE_URL")
	}
	if c.DevAuthUser == "" && c.OIDC.IssuerURL == "" {
		missing = append(missing, "BODEGA_OIDC_ISSUER")
	}
	if len(missing) > 0 {
		return c, fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
	}
	if c.OIDC.IdentityNamespace == "" {
		c.OIDC.IdentityNamespace = c.OIDC.IssuerURL
	}
	if c.DevAuthUser != "" && !c.InsecureCookies {
		return c, fmt.Errorf("BODEGA_DEV_AUTH_USER requires BODEGA_INSECURE_COOKIES=true (dev only)")
	}
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v, err := strconv.ParseBool(env(key, strconv.FormatBool(def)))
	if err != nil {
		return def
	}
	return v
}

func envInt(key string, def int64) int64 {
	v, err := strconv.ParseInt(env(key, ""), 10, 64)
	if err != nil {
		return def
	}
	return v
}

func envDuration(key string, def time.Duration) time.Duration {
	v, err := time.ParseDuration(env(key, ""))
	if err != nil {
		return def
	}
	return v
}
