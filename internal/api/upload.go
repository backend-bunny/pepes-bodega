package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"

	"github.com/backend-bunny/pepes-bodega/internal/db"
	"github.com/backend-bunny/pepes-bodega/internal/media"
	"github.com/backend-bunny/pepes-bodega/internal/queue"
	"github.com/backend-bunny/pepes-bodega/internal/storage"
)

var shaRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

const maxCheckHashes = 1000

// checkHashes lets clients skip uploading content the server already has.
func (s *Server) checkHashes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Hashes []string `json:"hashes"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		badRequest(w, "invalid json")
		return
	}
	if len(body.Hashes) > maxCheckHashes {
		badRequest(w, "too many hashes")
		return
	}
	for _, h := range body.Hashes {
		if !shaRe.MatchString(h) {
			badRequest(w, "hashes must be lowercase hex sha256")
			return
		}
	}
	st, err := s.Q.CheckHashes(r.Context(), user(r).ID, body.Hashes)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNil(st)})
}

type uploadResult struct {
	MediaID   int64  `json:"media_id"`
	SHA256    string `json:"sha256"`
	Kind      string `json:"kind"`
	Created   bool   `json:"created"`   // user did not own it before
	NewBlob   bool   `json:"new_blob"`  // content was new to the archive
	Duplicate bool   `json:"duplicate"` // exact duplicate of existing content
}

// upload accepts multipart/form-data with fields "device" (optional, first)
// and one or more "file" parts. Each file is spooled to disk while hashing,
// stored at its content address and queued for processing.
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	mr, err := r.MultipartReader()
	if err != nil {
		badRequest(w, "expected multipart/form-data")
		return
	}
	device := ""
	var results []uploadResult
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			badRequest(w, "malformed multipart body")
			return
		}
		switch part.FormName() {
		case "device":
			b, _ := io.ReadAll(io.LimitReader(part, 256))
			device = string(b)
		case "file":
			res, status, msg := s.ingest(r, part, part.FileName(), device)
			if status != 0 {
				writeJSON(w, status, map[string]any{"error": msg, "file": part.FileName(), "results": nonNil(results)})
				return
			}
			results = append(results, res)
		}
		_ = part.Close()
	}
	if len(results) == 0 {
		badRequest(w, "no file parts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// ingest returns a non-zero HTTP status and message on failure.
func (s *Server) ingest(r *http.Request, src io.Reader, filename, device string) (uploadResult, int, string) {
	ctx := r.Context()
	tmp, err := os.CreateTemp("", "bodega-upload-*")
	if err != nil {
		s.Log.Error("create temp", "err", err)
		return uploadResult{}, http.StatusInternalServerError, "internal error"
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(src, s.MaxUploadBytes+1))
	if err != nil {
		return uploadResult{}, http.StatusBadRequest, "upload interrupted"
	}
	if n > s.MaxUploadBytes {
		return uploadResult{}, http.StatusRequestEntityTooLarge, "file too large"
	}
	if n == 0 {
		return uploadResult{}, http.StatusBadRequest, "empty file"
	}
	sum := hex.EncodeToString(h.Sum(nil))

	head := make([]byte, 512)
	hn, _ := tmp.ReadAt(head, 0)
	mime, kind, ok := media.Detect(head[:hn], filename)
	if !ok {
		return uploadResult{}, http.StatusUnsupportedMediaType, "unsupported media type " + mime
	}

	res := uploadResult{SHA256: sum, Kind: kind}
	exists, err := s.Store.Exists(ctx, storage.BlobKey(sum))
	if err != nil {
		s.Log.Error("stat blob", "err", err)
		return res, http.StatusInternalServerError, "internal error"
	}
	if !exists {
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return res, http.StatusInternalServerError, "internal error"
		}
		if err := s.Store.Put(ctx, storage.BlobKey(sum), tmp, n, mime); err != nil {
			s.Log.Error("put blob", "err", err)
			return res, http.StatusBadGateway, "storage unavailable"
		}
	}
	res.NewBlob, err = s.Q.InsertBlob(ctx, db.NewBlob{SHA256: sum, Size: n, MimeType: mime, Kind: kind})
	if err != nil {
		s.Log.Error("insert blob", "err", err)
		return res, http.StatusInternalServerError, "internal error"
	}
	res.Duplicate = !res.NewBlob
	res.MediaID, res.Created, err = s.Q.LinkMedia(ctx, user(r).ID, sum, filename, device)
	if err != nil {
		s.Log.Error("link media", "err", err)
		return res, http.StatusInternalServerError, "internal error"
	}
	if res.NewBlob {
		// A failed publish is retried by the reconciler, so don't fail the upload.
		if err := s.Queue.PublishProcess(ctx, queue.ProcessJob{SHA256: sum, MimeType: mime, Kind: kind}); err != nil {
			s.Log.Warn("publish job; reconciler will retry", "sha256", sum, "err", err)
		}
	}
	return res, 0, ""
}
