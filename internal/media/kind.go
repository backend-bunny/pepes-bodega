// Package media classifies uploaded files.
package media

import (
	"net/http"
	"path/filepath"
	"strings"
)

// Kinds stored in blobs.kind.
const (
	KindImage     = "image"
	KindAnimation = "animation"
	KindVideo     = "video"
)

var extMIME = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png",
	".gif": "image/gif", ".webp": "image/webp", ".bmp": "image/bmp",
	".mp4": "video/mp4", ".m4v": "video/mp4", ".mov": "video/quicktime",
	".webm": "video/webm", ".mkv": "video/x-matroska",
}

// Detect sniffs the first bytes of a file (and falls back to its extension)
// and returns the MIME type and kind, or ok=false for unsupported files.
func Detect(head []byte, filename string) (mime, kind string, ok bool) {
	mime = http.DetectContentType(head)
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = mime[:i]
	}
	if mime == "application/octet-stream" || mime == "text/plain" {
		if m, found := extMIME[strings.ToLower(filepath.Ext(filename))]; found {
			mime = m
		}
	}
	switch {
	case mime == "image/gif":
		return mime, KindAnimation, true
	case strings.HasPrefix(mime, "image/"):
		return mime, KindImage, true
	case strings.HasPrefix(mime, "video/"):
		return mime, KindVideo, true
	}
	return mime, "", false
}

// Supported reports whether a filename has an extension the pipeline handles.
// The CLI uses this to skip obviously irrelevant files before hashing.
func Supported(filename string) bool {
	_, ok := extMIME[strings.ToLower(filepath.Ext(filename))]
	return ok
}
