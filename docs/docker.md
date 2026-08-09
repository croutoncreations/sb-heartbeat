# Docker

SB Heartbeat's container is a shell-free `scratch` image. It runs as numeric
user `65532:65532`, needs no writable filesystem, and includes only the static
CLI binary plus CA certificates for HTTPS.

No container image is published during local development. Build an exact local
version from a reviewed source checkout:

```bash
docker build \
  --build-arg VERSION=v0.1.1 \
  --tag sb-heartbeat:v0.1.1 \
  .
```

Mount configuration read-only and forward environment variables that already
exist in the shell. A flag such as `--env NAME` passes the existing value
without placing it in the command itself:

```bash
docker run --rm --read-only \
  --volume "$PWD/sb-heartbeat.yaml:/config/sb-heartbeat.yaml:ro" \
  --env SB_HEARTBEAT_MY_STAGE_URL \
  --env SB_HEARTBEAT_MY_STAGE_API_KEY \
  sb-heartbeat:v0.1.1 \
  --config /config/sb-heartbeat.yaml doctor
```

Use only a Supabase project URL and a publishable or legacy anon key with low
privilege. Never pass a database password, secret key, service-role key, or Management
API token. If an environment file is operationally necessary, keep it protected
outside the repository, never bake or copy it into the image, and pass its path
deliberately at runtime.

The Dockerfile uses BuildKit's target platform arguments. Verify both supported
Linux targets without publishing them:

```bash
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --build-arg VERSION=v0.1.1 \
  --output type=oci,dest=/tmp/sb-heartbeat-multiarch.tar \
  .
```

The repository integration script builds a local image, confirms its non-root
identity, runs version, completion, and migration generation with `--read-only`,
then produces and inspects a local multi-architecture OCI archive. Inspection
checks the exact platform set, runtime metadata, version labels, ELF machine
types, and absence of a dynamic interpreter:

```bash
scripts/integration-docker.sh
```

The script does not push an image or contact Supabase. It uses a digest-pinned
BuildKit executor, collision-checked random resource names, and creation flags
so cleanup removes only the temporary builder, image, and archive it created.
