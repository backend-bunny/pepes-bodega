#!/usr/bin/env bash
# Size the local dev stack to this machine. Prints JSON for the Tiltfile.
#
# Measured (CPU, ViT-B-32 + RapidOCR + Whisper base): a worker peaks at
# ~1.6 GiB RSS, the embedder at ~1.5 GiB. The rest of the dev stack (kind,
# Postgres, SeaweedFS, NATS, Dex, KEDA, API, embedder) plus the OS and Docker
# need ~4.5 GiB, and 2 cores stay free for them and ffmpeg.
#
# Overrides (any subset):
#   BODEGA_WORKERS_MAX     max worker pods KEDA may scale to
#   BODEGA_WORKER_THREADS  torch threads per worker (CPU each worker uses)
#   BODEGA_WORKER_MEMORY   worker memory limit, e.g. 3Gi
#   BODEGA_SEED_JOBS       parallel uploads for `seed`
set -euo pipefail

cores=$(getconf _NPROCESSORS_ONLN)
if [[ -r /proc/meminfo ]]; then
  mem_mib=$(awk '/^MemTotal:/ {print int($2 / 1024)}' /proc/meminfo)
else
  mem_mib=$(( $(sysctl -n hw.memsize) / 1048576 ))  # macOS
fi

reserve_mib=4608     # OS + docker + kind + deps + api + embedder
per_worker_mib=2048  # measured 1.6 GiB peak + headroom
threads=${BODEGA_WORKER_THREADS:-2}

by_mem=$(( (mem_mib - reserve_mib) / per_worker_mib ))
by_cpu=$(( (cores - 2) / threads ))
auto=$(( by_mem < by_cpu ? by_mem : by_cpu ))
(( auto < 1 )) && auto=1
workers=${BODEGA_WORKERS_MAX:-$auto}

jq -n \
  --argjson cores "$cores" --argjson mem_mib "$mem_mib" \
  --argjson workers "$workers" --argjson auto "$auto" --argjson threads "$threads" \
  --arg memory "${BODEGA_WORKER_MEMORY:-3Gi}" \
  --argjson seed_jobs "${BODEGA_SEED_JOBS:-$(( workers * 2 > 2 ? workers * 2 : 2 ))}" \
  '{cores: $cores, mem_mib: $mem_mib, workers_max: $workers, workers_auto: $auto,
    worker_threads: $threads, worker_memory: $memory, seed_jobs: $seed_jobs}'
