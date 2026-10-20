-- +goose Up
-- Live progress for the GUI: notify on every blob status change and on media
-- added/removed. The API LISTENs and pushes per-user updates over SSE
-- (internal/api/events.go). Payloads: "blob:<sha256>:<status>", "media:<owner_id>".
-- +goose StatementBegin
CREATE FUNCTION bodega_notify_blob() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('bodega_events', 'blob:' || NEW.sha256 || ':' || NEW.status);
    RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE TRIGGER blobs_notify_status
    AFTER INSERT OR UPDATE OF status, duplicate_of ON blobs
    FOR EACH ROW EXECUTE FUNCTION bodega_notify_blob();

-- +goose StatementBegin
CREATE FUNCTION bodega_notify_media() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('bodega_events', 'media:' || coalesce(NEW.owner_id, OLD.owner_id));
    RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE TRIGGER media_notify_change
    AFTER INSERT OR DELETE ON media
    FOR EACH ROW EXECUTE FUNCTION bodega_notify_media();

-- +goose Down
DROP TRIGGER media_notify_change ON media;
DROP TRIGGER blobs_notify_status ON blobs;
DROP FUNCTION bodega_notify_media();
DROP FUNCTION bodega_notify_blob();
