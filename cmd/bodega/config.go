package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/backend-bunny/pepes-bodega/internal/client"
)

type Config struct {
	Server string `json:"server"`
	Token  string `json:"token"`
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bodega", "config.json"), nil
}

func saveConfig(c Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(p, b, 0o600)
}

func newClient() (*client.Client, error) {
	var c Config
	if p, err := configPath(); err == nil {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &c)
		}
	}
	if v := os.Getenv("BODEGA_SERVER"); v != "" {
		c.Server = v
	}
	if v := os.Getenv("BODEGA_TOKEN"); v != "" {
		c.Token = v
	}
	if c.Server == "" || c.Token == "" {
		return nil, errors.New("not logged in: run `bodega login` or set BODEGA_SERVER and BODEGA_TOKEN")
	}
	return client.New(c.Server, c.Token), nil
}
