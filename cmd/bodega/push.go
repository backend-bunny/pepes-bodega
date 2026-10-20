package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/backend-bunny/pepes-bodega/internal/client"
	"github.com/backend-bunny/pepes-bodega/internal/media"
)

// fileState caches a file's hash keyed by path+size+mtime so unchanged files
// are not re-hashed on every scan, and whether it was uploaded to this server.
type fileState struct {
	Size     int64  `json:"size"`
	ModNano  int64  `json:"mod"`
	SHA256   string `json:"sha256"`
	Uploaded bool   `json:"uploaded"`
}

type pusher struct {
	client *client.Client
	device string
	jobs   int
	dryRun bool

	mu        sync.Mutex
	state     map[string]fileState
	statePath string
}

const checkBatch = 500

func (p *pusher) loadState() error {
	dir, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	// Per server: "uploaded" is only true for the server it was uploaded to. A
	// shared cache made a fresh server (new cluster, new URL) receive nothing.
	sum := sha256.Sum256([]byte(p.client.Server))
	p.statePath = filepath.Join(dir, "bodega", "state-"+hex.EncodeToString(sum[:6])+".json")
	p.state = map[string]fileState{}
	b, err := os.ReadFile(p.statePath)
	if err == nil {
		_ = json.Unmarshal(b, &p.state)
	}
	return nil
}

func (p *pusher) saveState() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(p.statePath), 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(p.state)
	tmp := p.statePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p.statePath)
}

type candidate struct {
	path string
	st   fileState
}

func (p *pusher) run(ctx context.Context, roots []string) error {
	cands, err := p.scan(ctx, roots)
	if err != nil {
		return err
	}
	var todo []candidate
	for _, c := range cands {
		if !c.st.Uploaded {
			todo = append(todo, c)
		}
	}
	// Ask the server which of the remaining hashes we already own (e.g. uploaded
	// from another device) so they are not sent again.
	for i := 0; i < len(todo); i += checkBatch {
		batch := todo[i:min(i+checkBatch, len(todo))]
		hashes := make([]string, len(batch))
		for j, c := range batch {
			hashes[j] = c.st.SHA256
		}
		owned, err := p.client.Owned(ctx, hashes)
		if err != nil {
			return fmt.Errorf("check hashes: %w", err)
		}
		for j := range batch {
			if owned[batch[j].st.SHA256] {
				batch[j].st.Uploaded = true
				p.setState(batch[j].path, batch[j].st)
			}
		}
	}
	// Identical files in the scan only need one upload; the rest are marked
	// uploaded once it succeeds (next run sees them as owned).
	var upload []candidate
	queued := map[string]bool{}
	for _, c := range todo {
		if !c.st.Uploaded && !queued[c.st.SHA256] {
			queued[c.st.SHA256] = true
			upload = append(upload, c)
		}
	}
	fmt.Printf("%d files scanned, %d already in library, %d identical copies, %d to upload\n",
		len(cands), len(cands)-len(todo), len(todo)-len(upload), len(upload))
	if p.dryRun {
		for _, c := range upload {
			fmt.Println("would upload", c.path)
		}
		return nil
	}
	err = p.uploadAll(ctx, upload)
	if serr := p.saveState(); serr != nil && err == nil {
		err = serr
	}
	return err
}

func (p *pusher) setState(path string, st fileState) {
	p.mu.Lock()
	p.state[path] = st
	p.mu.Unlock()
}

func (p *pusher) scan(ctx context.Context, roots []string) ([]candidate, error) {
	var out []candidate
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				fmt.Fprintln(os.Stderr, "skip:", err)
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if d.IsDir() || !d.Type().IsRegular() || !media.Supported(path) {
				return nil
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			st, ok := p.state[abs]
			if !ok || st.Size != info.Size() || st.ModNano != info.ModTime().UnixNano() {
				sum, err := hashFile(abs)
				if err != nil {
					fmt.Fprintln(os.Stderr, "skip:", err)
					return nil
				}
				st = fileState{Size: info.Size(), ModNano: info.ModTime().UnixNano(), SHA256: sum}
				p.setState(abs, st)
			}
			out = append(out, candidate{path: abs, st: st})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (p *pusher) uploadAll(ctx context.Context, cands []candidate) error {
	work := make(chan candidate)
	var done, failed atomic.Int64
	var wg sync.WaitGroup
	for range p.jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range work {
				res, err := p.client.Upload(ctx, c.path, p.device)
				n := done.Add(1)
				if err != nil {
					failed.Add(1)
					fmt.Fprintf(os.Stderr, "[%d/%d] FAIL %s: %v\n", n, len(cands), c.path, err)
					continue
				}
				c.st.Uploaded = true
				p.setState(c.path, c.st)
				note := ""
				if res.Duplicate {
					note = " (content already in archive)"
				}
				fmt.Printf("[%d/%d] %s → #%d%s\n", n, len(cands), filepath.Base(c.path), res.MediaID, note)
			}
		}()
	}
loop:
	for _, c := range cands {
		select {
		case work <- c:
		case <-ctx.Done():
			break loop
		}
	}
	close(work)
	wg.Wait()
	if f := failed.Load(); f > 0 {
		return fmt.Errorf("%d uploads failed", f)
	}
	return ctx.Err()
}
