# Docker

SB Heartbeat's container is a shell-free `scratch` image. It runs as numeric
user `65532:65532`, needs no writable filesystem, and includes only the static
CLI binary plus CA certificates for HTTPS.

No container image is published during local development. Build a development
image from the current reviewed source checkout without claiming a released
version:

```bash
docker build \
  --build-arg VERSION=devel \
  --tag sb-heartbeat:devel \
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
  sb-heartbeat:devel \
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
  --build-arg VERSION=devel \
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

## Release images

Release tags publish the same shell-free image to GitHub Container Registry for
`linux/amd64` and `linux/arm64`, after the binary release and all release gates
succeed. Pull an exact release tag; no mutable `latest` tag is published:

```bash
docker pull ghcr.io/croutoncreations/sb-heartbeat:v0.2.0
```

For deployment, resolve that tag and pin the resulting digest, for example
`ghcr.io/croutoncreations/sb-heartbeat@sha256:...`. Each release build includes
a BuildKit provenance record and SBOM, plus a GitHub artifact attestation bound
to the pushed manifest digest. After authenticating Docker to GHCR, verify it
against this repository:

```bash
gh attestation verify oci://ghcr.io/croutoncreations/sb-heartbeat:v0.2.0 -R croutoncreations/sb-heartbeat
```

The release workflow pushes content by digest, attests that digest, and only
then creates the advertised version tag. It refuses to replace an existing
version tag, and it treats authentication, registry, rate-limit, and network
inspection failures as fatal rather than assuming the tag is absent.
Publication uses the repository-scoped `GITHUB_TOKEN`; it does not require a
personal access token. The first package is expected to be private. A
repository owner must make the package visibility public once and confirm it is
linked to this repository before advertising anonymous pulls. Visibility is
repository administration, not something the release workflow changes
automatically.
