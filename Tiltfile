# Local dev loop on kind. Run `devbox run cluster:up` once, then
# `devbox run dev` (localhost) or `devbox run dev:netbird` (also exposed on NetBird).
allow_k8s_contexts('kind-pepes-bodega')

# URLs the *browser* uses. hack/dev-netbird.sh sets these to the
# netbird-exposed https URLs; otherwise everything is on localhost.
public_url = os.getenv('BODEGA_PUBLIC_URL', 'http://localhost:8080')
dex_issuer = os.getenv('BODEGA_DEX_PUBLIC_URL', 'http://localhost:5556')

# Worker pool sized to this machine (hack/dev-sizing.sh); override with
# BODEGA_WORKERS_MAX / BODEGA_WORKER_THREADS / BODEGA_WORKER_MEMORY / BODEGA_SEED_JOBS.
sizing = decode_json(str(local('hack/dev-sizing.sh', quiet=True, echo_off=True)))
print('dev sizing: %d cores, %d MiB -> up to %d worker(s) x %d thread(s), %s limit (auto: %d)' % (
    sizing['cores'], sizing['mem_mib'], sizing['workers_max'], sizing['worker_threads'],
    sizing['worker_memory'], sizing['workers_auto']))

# ---- backing services: postgres, seaweedfs, nats, dex ----
deps = str(read_file('deploy/dev/deps.yaml'))
deps = deps.replace('__DEX_ISSUER__', dex_issuer).replace('__BODEGA_PUBLIC_URL__', public_url)
k8s_yaml(blob(deps))
k8s_resource('postgres', labels=['deps'])
k8s_resource('seaweedfs', labels=['deps'])
k8s_resource('nats', labels=['deps'])
k8s_resource('dex', labels=['deps'], links=[link(dex_issuer + '/.well-known/openid-configuration', 'dex')])

# ---- images ----
docker_build(
    'bodega-api', '.',
    dockerfile='deploy/docker/api.Dockerfile',
    only=['go.mod', 'go.sum', 'cmd', 'internal', 'web'],
    ignore=['web/node_modules', 'web/.svelte-kit', 'web/build'],
)
docker_build(
    'bodega-worker', '.',
    dockerfile='deploy/docker/worker.Dockerfile',
    only=['workers/pyproject.toml', 'workers/uv.lock', 'workers/bodega_workers'],
    # Baked models make scale-from-zero fast; the layer is cached after the first build.
    build_args={'PREFETCH_MODELS': 'true'},
)

# ---- app ----
k8s_yaml(helm(
    'deploy/helm/pepes-bodega', name='bodega',
    values=['deploy/helm/values-dev.yaml'],
    set=[
        'config.publicURL=' + public_url,
        'config.oidc.issuer=' + dex_issuer,
        'workers.autoscaling.maxReplicas=%d' % sizing['workers_max'],
        'workers.torchThreads=%d' % sizing['worker_threads'],
        'workers.resources.limits.memory=' + sizing['worker_memory'],
    ],
))
links = [link('http://localhost:8080', 'localhost (pepe@bodega.local / sangria)')]
if public_url != 'http://localhost:8080':
    links.insert(0, link(public_url, 'netbird (pepe@bodega.local / sangria)'))
k8s_resource('bodega-api', labels=['app'], resource_deps=['postgres', 'seaweedfs', 'nats', 'dex'], links=links)
k8s_resource('bodega-embedder', labels=['app'], resource_deps=['nats'])
k8s_resource('bodega-worker', labels=['app'], resource_deps=['bodega-api'],
             objects=['bodega-worker:scaledobject'])

# ---- helpers ----
local_resource('web-dev', serve_cmd='cd web && npm run dev', labels=['tools'], auto_init=False,
               links=['http://localhost:5173'])
local_resource('seed', cmd='hack/seed.sh', labels=['tools'], auto_init=False,
               trigger_mode=TRIGGER_MODE_MANUAL, resource_deps=['bodega-api'],
               env={'BODEGA_PUBLIC_URL': public_url, 'BODEGA_DEX_PUBLIC_URL': dex_issuer,
                    'BODEGA_SEED_JOBS': str(sizing['seed_jobs'])})
local_resource('tests', cmd='hack/test.sh', labels=['tools'], auto_init=False,
               trigger_mode=TRIGGER_MODE_MANUAL)
