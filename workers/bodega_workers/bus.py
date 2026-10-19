"""NATS subjects shared with the Go API (internal/queue/queue.go). Keep in sync."""

STREAM_JOBS = "BODEGA_JOBS"
SUBJECT_PROCESS = "bodega.jobs.process"
CONSUMER_PROCESS = "process-workers"
SUBJECT_EMBED_TEXT = "bodega.embed.text"
EMBEDDER_QUEUE_GROUP = "embedders"
