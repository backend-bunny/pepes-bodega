// Package client is a small Go client for the bodega HTTP API, used by the CLI.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Client struct {
	Server string
	Token  string
	HTTP   *http.Client
}

func New(server, token string) *Client {
	return &Client{
		Server: strings.TrimRight(server, "/"),
		Token:  token,
		HTTP:   &http.Client{Timeout: 30 * time.Minute},
	}
}

type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string { return fmt.Sprintf("api: %d %s", e.Status, strings.TrimSpace(e.Body)) }

func (c *Client) do(ctx context.Context, method, path, contentType string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.Server+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &APIError{Status: resp.StatusCode, Body: string(b)}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type User struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (c *Client) Me(ctx context.Context) (User, error) {
	var u User
	return u, c.do(ctx, http.MethodGet, "/api/v1/me", "", nil, &u)
}

// Owned returns the subset of hashes the user already has in their library.
func (c *Client) Owned(ctx context.Context, hashes []string) (map[string]bool, error) {
	body, _ := json.Marshal(map[string]any{"hashes": hashes})
	var out struct {
		Items []struct {
			SHA256 string `json:"sha256"`
			Owned  bool   `json:"owned"`
		} `json:"items"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/media/check", "application/json", bytes.NewReader(body), &out); err != nil {
		return nil, err
	}
	owned := make(map[string]bool, len(out.Items))
	for _, it := range out.Items {
		if it.Owned {
			owned[it.SHA256] = true
		}
	}
	return owned, nil
}

type UploadResult struct {
	MediaID   int64  `json:"media_id"`
	SHA256    string `json:"sha256"`
	Kind      string `json:"kind"`
	Created   bool   `json:"created"`
	NewBlob   bool   `json:"new_blob"`
	Duplicate bool   `json:"duplicate"`
}

// Upload streams one file as multipart without buffering it in memory.
func (c *Client) Upload(ctx context.Context, path, device string) (UploadResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return UploadResult{}, err
	}
	defer f.Close()

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		err := func() error {
			if err := mw.WriteField("device", device); err != nil {
				return err
			}
			fw, err := mw.CreateFormFile("file", filepath.Base(path))
			if err != nil {
				return err
			}
			if _, err := io.Copy(fw, f); err != nil {
				return err
			}
			return mw.Close()
		}()
		pw.CloseWithError(err)
	}()

	var out struct {
		Results []UploadResult `json:"results"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/media", mw.FormDataContentType(), pr, &out); err != nil {
		return UploadResult{}, err
	}
	if len(out.Results) != 1 {
		return UploadResult{}, fmt.Errorf("unexpected upload response with %d results", len(out.Results))
	}
	return out.Results[0], nil
}
