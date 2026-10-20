# DALI Release Deployment

Release `1.2.3-dali.1` is based on upstream `v1.2.3` and merged fork PR #8. It
includes shared registry pull Secrets, native Slurm mail annotations, and the
reviewed PodGroup mail override fix. This release adds immutable image digest
support and build provenance metadata.

This is a prerelease. Fork issue #5 remains unresolved: successful workload
cleanup can report a cancelled Slurm allocation and cancellation notifications.
The new artifact digests require live acceptance before production use. The
previously reported Slurm 25.11.5 / Kubernetes 1.34 environment does not certify
these new artifacts. Built-in PodGroups require Kubernetes 1.36.

## Argo CD

The GitHub release provides a chart package, checksums, `release-manifest.json`,
`release-images.json`, and `argocd-source.yaml`:

1. Verify downloaded assets against `SHA256SUMS`.
1. In the infrastructure repository, retain the existing Application's project,
   destination, release name, environment values, and sync policy. Change its
   chart source to this fork, path `helm/slurm-bridge`, and the full source SHA
   recorded in `release-manifest.json`. Do not track the mutable deployment
   branch.
1. Merge the three component image entries from `release-images.json` into the
   existing Helm values. Alternatively, merge the matching `helm.valuesObject`
   entries from `argocd-source.yaml`. The latter is a source configuration
   snippet, not a standalone Application or a manifest to apply with kubectl.
   Preserve all other environment-specific values and any multi-source value
   references.
1. Preserve the existing `imagePullSecrets`, externally managed Slurm credential
   Secret reference, Slurm endpoint/partition, namespace admission, and RBAC
   configuration. Registry pull Secrets must exist in the deployment namespace.
   Do not put registry passwords or Slurm JWTs in values or the Application.
1. Review the Argo CD diff, then sync in an approved window. Confirm that the
   scheduler, controllers, and admission use the recorded image digests and are
   healthy. Run an admitted workload canary and the mail/allocation checks in
   [testing](./testing.md#dali-mail-validation).

Upgrade all three components together. A nonempty `image.digest` takes
precedence over `image.tag`; without a digest, explicit tags and the
chart-version fallback remain supported. Chart defaults still name upstream
repositories, so the release image values must be supplied. Argo CD repository
access and workload image pull credentials remain separately managed
infrastructure requirements.

Retain the prior source SHA, image digests, environment values, and a valid
Slurm credential reference for rollback. Reverting GitOps values does not
correct the known terminal-outcome behavior or restore an expired credential.
Release creation does not deploy anything or change the infrastructure
repository.

## Image Build

Build from the clean release commit, using `VERSION=1.2.3-dali.1`, the approved
`REGISTRY`, `SOURCE=https://github.com/luiggilopezee/slurm-bridge`, and
`REVISION=$(git rev-parse HEAD)` with Docker Bake. Publish both `linux/amd64`
and `linux/arm64` with `--sbom=true --provenance=mode=max`, and retain the
metadata.

For corporate TLS proxies, trust the approved public CA in the BuildKit daemon
and set `BUILD_CA_FILE` to its absolute PEM bundle path. Grant Bake read access
only to that path when outside the context. The bundle is mounted as an optional
secret during Go download/build steps and is not copied into runtime images. Do
not disable TLS verification or upload the bundle as a release asset.

Inspect all six published component/platform binaries with `go version` before
recording the compiler. The Docker builder tag `golang:1.26` is not an exact
compiler pin, and the module minimum or host test toolchain is not build
evidence. Record source/revision OCI labels, index and platform digests, chart
checksum, SBOM/provenance references, and actual acceptance gaps. Attestations
are not publisher signatures or proof of bit-for-bit reproducibility.
