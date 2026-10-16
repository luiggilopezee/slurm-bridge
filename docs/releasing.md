# Fork Releases

This procedure prepares the immutable release required by issue #6. It does not
declare an unreleased working tree production-ready. Publication is gated on
issue #5's terminal-outcome fix and the
[bridge contract acceptance run](./testing.md#bridge-contract-acceptance). Do
not publish a successful-completion claim while cleanup still cancels successful
allocations.

## Release Identity and Compatibility

Use a distinct fork version, for example `1.2.2-dali.1`, rather than overwriting
upstream `1.2.2` images or a previously published release. Before building,
update `VERSION`, run `make version-match`, regenerate chart documentation, and
commit the reviewed changes through the normal contribution workflow. Choose the
actual version with the release owner; the example is not an allocated release
number.

The current baseline tag is `v1.2.2`, commit
`004c2401b7dc73dead96bdb94e0379a3befe6800`. Record that commit explicitly;
neither a version-looking branch name nor the upstream module path identifies
the fork's actual source. A release manifest must contain:

| Field             | Required evidence                                                                                                      |
| ----------------- | ---------------------------------------------------------------------------------------------------------------------- |
| Fork source       | Repository URL and full reviewed Git commit SHA                                                                        |
| Upstream baseline | Tag and full baseline SHA; note any additional upstream merges                                                         |
| Chart             | Version, `appVersion`, packaged artifact checksum, and artifact location                                               |
| Images            | Scheduler, controllers, and admission repositories and multi-platform manifest digests                                 |
| Build             | Go toolchain, build command, target platforms, source-revision OCI labels, SBOM/signature references                   |
| Compatibility     | Tested Kubernetes and Slurm versions, REST API/plugin version, Slurm client version, GPU/DRA drivers and grouping APIs |
| Acceptance        | Sanitized results for each required case, including skips and unresolved blockers                                      |

The module requires Go 1.26.6 and uses Kubernetes 1.36.0 libraries and
`slurm-client v1.2.2` with v0.0.44 external jobs. The image builder uses the
floating `golang:1.26` image; the Makefile's host `GOTOOLCHAIN` does not pin the
compiler inside Docker. Record the compiler verified from the candidate
binaries, not the module requirement or the host's `go version`. The chart
declares Kubernetes `>=1.34.0-0` and Slurm 25.11 minimums, while the overview
currently states Kubernetes 1.35. These are not equivalent to a verified support
matrix. Until those declarations are reconciled using test evidence, state
support only for the exact tested combinations. Built-in PodGroups require the
1.36 API and feature configuration. Do not imply every version above the chart
minimum was certified.

## Build and Record Artifacts

Run from a clean, reviewed checkout with the required Go/build/Helm tools,
registry credentials supplied by the operator, and an approved fork registry. Do
not include credentials in command output or release files. The commands below
**push candidate images**; run them only as the authorized release operator. Run
local checks first, then deploy these exact candidate digests to the isolated
acceptance environment. Publish the release record only after live acceptance
passes; do not rebuild between acceptance and release publication.

```sh
set -euo pipefail
test -z "$(git status --porcelain)"
SOURCE=https://github.com/luiggilopezee/slurm-bridge
REVISION=$(git rev-parse HEAD)
UPSTREAM_REVISION=$(git rev-parse 'v1.2.2^{commit}')
VERSION=$(tr -d '\n' < VERSION)
: "${REGISTRY:?Set the approved fork registry and organization}"
make helm-unittest
make helm-validate
go test ./internal/utils/slurmjobir ./internal/scheduler/plugins/slurmbridge/slurmcontrol
make push-images REGISTRY="$REGISTRY" SOURCE="$SOURCE" REVISION="$REVISION"
make build-chart
shasum -a 256 "slurm-bridge-$VERSION.tgz"
```

The image build emits `bake-metadata.json`. Generate a credential-free manifest
from that file and the tested environment versions. Only copy the selected
fields below, not registry credential files or whole environment dumps.

Before generating the manifest, inspect each component binary from each
published platform at its candidate image digest. For example, the following
extracts the scheduler without executing the container or its binary:

```sh
COMPONENT=scheduler
PLATFORM=linux/amd64
DIGEST=$(jq -er --arg component "$COMPONENT" \
  '.[$component]["containerimage.digest"]' bake-metadata.json)
IMAGE="$REGISTRY/slurm-bridge-$COMPONENT@$DIGEST"
docker pull --platform "$PLATFORM" "$IMAGE"
BINARY_DIR=$(mktemp -d)
CONTAINER=$(docker create --platform "$PLATFORM" "$IMAGE")
docker cp "$CONTAINER:/$COMPONENT" "$BINARY_DIR/$COMPONENT"
docker rm "$CONTAINER"
go version "$BINARY_DIR/$COMPONENT"
rm -r "$BINARY_DIR"
```

Repeat for scheduler, controllers, and admission on both `linux/amd64` and
`linux/arm64`. Save the image digest, platform, component, and reported compiler
version in the release evidence. Set `BUILD_GO_VERSION` to the reported version
(including its `go` prefix) only after confirming all six binaries agree. If
they differ, stop and reconcile the build or extend the manifest to record
per-component/platform compiler versions instead of claiming one common value.
The local `go version <binary>` command reads embedded build information and
does not execute the foreign-platform binary.

```sh
: "${TESTED_KUBERNETES:?Set the Kubernetes version from the acceptance run}"
: "${TESTED_SLURM:?Set the Slurm version from the acceptance run}"
: "${BUILD_GO_VERSION:?Set the compiler version verified from every candidate binary}"
CHART_SHA256=$(shasum -a 256 "slurm-bridge-$VERSION.tgz" | awk '{print $1}')
jq -e '.["scheduler"]["containerimage.digest"] and
       .["controllers"]["containerimage.digest"] and
       .["admission"]["containerimage.digest"]' bake-metadata.json >/dev/null
jq --arg source "$SOURCE" --arg revision "$REVISION" \
  --arg upstream "$UPSTREAM_REVISION" --arg version "$VERSION" \
  --arg registry "$REGISTRY" --arg chartSHA256 "$CHART_SHA256" \
  --arg kubernetes "$TESTED_KUBERNETES" --arg slurm "$TESTED_SLURM" \
  --arg buildGo "$BUILD_GO_VERSION" '
  . as $bake | {
    source: {repository: $source, revision: $revision},
    upstream: {tag: "v1.2.2", revision: $upstream},
    version: $version,
    chart: {version: $version, appVersion: $version,
            artifact: ("slurm-bridge-" + $version + ".tgz"), sha256: $chartSHA256},
    build: {go: $buildGo, platforms: ["linux/amd64", "linux/arm64"]},
    tested: {kubernetes: $kubernetes, slurm: $slurm,
             slurmClient: "v1.2.2", slurmAPI: "v0.0.44"},
    images: (["scheduler", "controllers", "admission"] | map(
      . as $component | {key: $component, value: {
        repository: ($registry + "/slurm-bridge-" + $component),
        digest: $bake[$component]["containerimage.digest"]}}) | from_entries)
  }' bake-metadata.json > release-manifest.json
jq '.images | with_entries(.value = {image: .value})' \
  release-manifest.json > release-images.json
```

`release-images.json` is a Helm values file: JSON is valid YAML. It pins all
three images by digest without recording a token. Add artifact locations,
driver/API configuration, sanitized acceptance results, and SBOM/signature
references to the release record. Inspect the published manifest lists and OCI
source/revision labels; verify both architectures claimed by the release. Build
metadata provides traceability, not proof of bit-for-bit reproducible builds.

The existing `sign-images` target rebuilds/pushes before signing. If using it,
run it before live acceptance and capture metadata from that final build. Do not
regenerate images after acceptance and silently reuse an older test record.
Publish the reviewed source tag, chart package/checksum, release manifest,
pinned image values, and sanitized acceptance report together. Do not change the
mutable deployment branch as a substitute for publishing these artifacts.

## Component Upgrade Contract

Native-mail submission is implemented by the **scheduler**; terminal outcome and
allocation cleanup are implemented by **controllers**. Upgrade those two
together to the tested release. Updating only the scheduler cannot repair
successful-workload cancellation. Publish and deploy **admission** from the same
release as well so resource validation and the documented contract match.
Mixed-version combinations require their own acceptance evidence and are not
implicitly supported.

The chart accepts `scheduler.image.digest`, `controllers.image.digest`, and
`admission.image.digest` as `sha256:...`. A non-empty digest takes precedence
over `image.tag`, rendering `repository@digest`. Without a digest, the explicit
tag or chart version remains the fallback. Use registry-provided digests, not
guessed values or digests of locally exported image archives.

## Downstream Installation

Download the reviewed chart artifact and verify its SHA-256 against the trusted
release record. Supply environment-specific namespace admission, Slurm endpoint,
and the existing Secret **name** in a separate values file. Then install using
the pinned image values:

```sh
: "${VERSION:?Set the selected fork release version}"
: "${BRIDGE_NAMESPACE:?Set the bridge deployment namespace}"
helm upgrade --install slurm-bridge "./slurm-bridge-$VERSION.tgz" \
  --namespace "$BRIDGE_NAMESPACE" \
  -f environment-values.yaml -f release-images.json --wait --timeout=5m
```

Provision the namespace and externally managed Secret before installation. For
GitOps using a source checkout instead, pin `targetRevision` to the full release
commit SHA and keep `path: helm/slurm-bridge`; apply the same immutable image
values. A branch such as `deploy/dali-1.2.2` is not an immutable revision. Chart
SemVer alone also does not prevent a registry artifact being overwritten; retain
its trusted checksum or digest.

Changing Argo CD source pins, admitting `boltz2-dev`/`boltz2`, and provisioning
credentials remain changes in `ncats/sci-admin`. Application indexing/download
acceptance remains in `ncats/boltz2-dali`. Roll out in an approved window,
verify all three image identities and credential health, and rerun the canary
before resuming submissions. Keep the previous release manifest and still-valid
credential reference for recovery; never roll back to an expired credential.
