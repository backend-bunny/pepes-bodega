---
name: kind-dev
description: Run and debug Pepes Bodega on the local kind cluster with Tilt — cluster bootstrap, seeding sample data, inspecting pods, KEDA scaling, Postgres/NATS/SeaweedFS access and OIDC (Dex) problems. Use when the user asks to start the dev environment, seed data, or debug something in kind.
---

# Local kind development

Docker isn't reachable from the agent sandbox. If a command needs Docker
(`kind`, `tilt up`, image builds), ask the human to run it with the `!` prefix,
e.g. `! devbox run cluster:up`, so the output lands in the conversation.

## Bring up

```sh
devbox run cluster:up   # kind + KEDA (idempotent)
devbox run dev          # tilt up → http://localhost:8080, Tilt UI on :10350
devbox run dev:netbird  # same + netbird expose of app, Dex and Tilt UI (URLs printed at start, random per run)
devbox run seed         # or trigger the "seed" resource in Tilt
```
Login: `pepe@bodega.local` / `sangria`. The first worker image build bakes
models (~2 GB layer, cached afterwards).

## Inspect (namespace `bodega-dev` is the context default)

```sh
kubectl get pods,scaledobject
kubectl logs deploy/bodega-api -f
kubectl logs -l app.kubernetes.io/component=worker -f --max-log-requests 10
psql postgres://bodega:bodega@localhost:5432/bodega -c "select status, count(*) from blobs group by 1"
nats --server nats://localhost:4222 consumer info BODEGA_JOBS process-workers   # needs: kubectl port-forward svc/nats 4222
curl -s localhost:8222/jsz?consumers=true | jq   # JetStream lag as KEDA sees it
```

## Common problems

- **Workers stay at 0**: check `kubectl describe scaledobject bodega-worker`;
  KEDA reads `nats.bodega-dev.svc.cluster.local:8222`. The consumer only exists
  after the API has started once.
- **OIDC redirect/issuer errors**: the browser must use exactly the issuer Dex
  was rendered with (`kubectl get cm dex -o yaml | grep issuer`), and the API
  must have the same `BODEGA_OIDC_ISSUER` plus `BODEGA_OIDC_INTERNAL_URL=http://dex:5556`
  (`kubectl get deploy bodega-api -o yaml | grep -A1 OIDC`). Switching between
  `dev` and `dev:netbird` re-renders both; Dex rolls via its `bodega/issuer` annotation.
- **NetBird expose fails**: `dev:netbird` prints netbird's error. Check the
  client with `netbird-kbpp status`. Other peers also need a NetBird access
  policy that lets them reach this peer's exposed services. Restrict access with
  `BODEGA_NETBIRD_EXPOSE_ARGS="--with-user-groups <group>"`.
- **502 from the NetBird URL**: the proxy connects to this peer's NetBird IP,
  and kind only listens on 127.0.0.1. `dev:netbird` starts socat forwarders for
  that; check them with `ss -ltn | grep <netbird-ip>`. Don't test the URL from
  the exposing host itself (it returns 421); use another peer.
- **URL stops working after a few minutes**: look for
  `renewing expose session ... NotFound` in `journalctl -u netbird-kbpp`. The
  session was dropped on the management side; restart `dev:netbird`.
- **Login from another peer bounces to localhost**: you started plain `dev`;
  use `dev:netbird` so the public URLs are the exposed ones.
- **Blobs stuck `pending`**: the reconciler re-publishes them only when the queue is idle (every minute).
- **Re-process everything** (e.g. after a pipeline change):
  `psql … -c "update blobs set status='pending'"`, then wait for the reconciler.
- Reset: `devbox run cluster:down && devbox run cluster:up`.
