// Package queue talks to NATS JetStream: it publishes processing jobs and
// asks the Python embedder for query embeddings over request/reply.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Subjects and stream names shared with the Python workers (workers/bodega_workers/bus.py).
const (
	StreamJobs      = "BODEGA_JOBS"
	SubjectProcess  = "bodega.jobs.process"
	ConsumerProcess = "process-workers"
	SubjectEmbedTxt = "bodega.embed.text"
)

// ProcessJob asks a worker to run the full extraction pipeline for a blob.
type ProcessJob struct {
	SHA256   string `json:"sha256"`
	MimeType string `json:"mime_type"`
	Kind     string `json:"kind"`
}

type Client struct {
	nc *nats.Conn
	js jetstream.JetStream
}

func Connect(ctx context.Context, url string) (*Client, error) {
	nc, err := nats.Connect(url, nats.Name("bodega-api"), nats.MaxReconnects(-1))
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, err
	}
	c := &Client{nc: nc, js: js}
	if err := c.ensureTopology(ctx); err != nil {
		nc.Close()
		return nil, err
	}
	return c, nil
}

// ensureTopology creates the work-queue stream and the shared pull consumer.
// Defining the consumer here (not in workers) gives KEDA a stable consumer to
// measure lag on before any worker pod exists.
func (c *Client) ensureTopology(ctx context.Context) error {
	_, err := c.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       StreamJobs,
		Subjects:   []string{"bodega.jobs.>"},
		Retention:  jetstream.WorkQueuePolicy,
		Storage:    jetstream.FileStorage,
		Duplicates: 10 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("create stream: %w", err)
	}
	_, err = c.js.CreateOrUpdateConsumer(ctx, StreamJobs, jetstream.ConsumerConfig{
		Durable:       ConsumerProcess,
		FilterSubject: SubjectProcess,
		AckPolicy:     jetstream.AckExplicitPolicy,
		// Videos can take minutes on CPU; workers send in-progress acks.
		AckWait:    5 * time.Minute,
		MaxDeliver: 5,
		BackOff:    []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute},
	})
	if err != nil {
		return fmt.Errorf("create consumer: %w", err)
	}
	return nil
}

// PublishProcess enqueues a job. The message ID makes re-publishing the same
// blob within the dedup window a no-op.
func (c *Client) PublishProcess(ctx context.Context, job ProcessJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	_, err = c.js.Publish(ctx, SubjectProcess, data, jetstream.WithMsgID("process-"+job.SHA256))
	return err
}

type embedRequest struct {
	Text string `json:"text"`
}

type embedReply struct {
	Embedding []float32 `json:"embedding"`
	Error     string    `json:"error,omitempty"`
}

// EmbedText returns the L2-normalised CLIP text embedding for a search query.
func (c *Client) EmbedText(ctx context.Context, text string) ([]float32, error) {
	data, _ := json.Marshal(embedRequest{Text: text})
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	msg, err := c.nc.RequestWithContext(ctx, SubjectEmbedTxt, data)
	if err != nil {
		return nil, fmt.Errorf("embed request: %w", err)
	}
	var reply embedReply
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		return nil, err
	}
	if reply.Error != "" {
		return nil, errors.New(reply.Error)
	}
	return reply.Embedding, nil
}

func (c *Client) Ping() error {
	if !c.nc.IsConnected() {
		return errors.New("nats not connected")
	}
	return nil
}

func (c *Client) Close() { c.nc.Close() }

// Idle reports whether the process consumer has no queued or in-flight
// messages. The reconciler only re-publishes when idle, so a long backlog
// never accumulates duplicate jobs beyond the dedup window.
func (c *Client) Idle(ctx context.Context) (bool, error) {
	cons, err := c.js.Consumer(ctx, StreamJobs, ConsumerProcess)
	if err != nil {
		return false, err
	}
	info, err := cons.Info(ctx)
	if err != nil {
		return false, err
	}
	return info.NumPending == 0 && info.NumAckPending == 0, nil
}
