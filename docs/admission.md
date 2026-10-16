# Admission

## Table of Contents

<!-- mdformat-toc start --slug=github --no-anchors --maxlevel=6 --minlevel=1 -->

- [Admission](#admission)
  - [Table of Contents](#table-of-contents)
  - [Overview](#overview)
  - [Design](#design)
    - [Namespace Selection Contract](#namespace-selection-contract)
    - [Sequence Diagram](#sequence-diagram)

<!-- mdformat-toc end -->

## Overview

[The Kubernetes documentation](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/)
defines admission controllers as:

> a piece of code that intercepts requests to the Kubernetes API server prior to
> persistence of the resource, but after the request is authenticated and
> authorized.

It also states that:

> Admission control mechanisms may be validating, mutating, or both. Mutating
> controllers may modify the data for the resource being modified; validating
> controllers may not.

The `slurm-bridge` admission controller is both mutating and validating. It
modifies pods within namespaces specified in `helm/slurm-bridge/values.yaml` to
use the `slurm-bridge` [scheduler] instead of the default Kubernetes scheduler,
and rejects unsupported resource combinations.

## Design

Pods created in selected namespaces with the default Kubernetes scheduler have
their `.spec.schedulerName` changed to the slurm-bridge [scheduler]. An explicit
different, non-default scheduler is not overwritten.

Managed namespaces are defined as a list of namespace as configured in the
admission controller's `values.yaml` for `managedNamespaces[]`. Alternatively, a
`managedNamespaceSelector` can be used to select namespaces based on labels. If
`managedNamespaceSelector` is set, `managedNamespaces` will be ignored.

Managed pods can request either native `cpu` or the CPU DRA extended resource
`deviceclass.resource.kubernetes.io/dra.cpu`, but cannot specify both. CPU DRA
must be requested explicitly; native `cpu` requests are not converted to DRA
requests by the admission controller.

### Namespace Selection Contract

`admission.managedNamespaces` defaults to `[slurm-bridge]`. It is a list of
namespace names, not the namespace where the bridge itself must be installed. A
non-empty `admission.managedNamespaceSelector` takes precedence over the entire
list. The chart default `{}` omits the selector and uses the list. Use
Kubernetes `matchLabels` or `matchExpressions`; do not use an empty selector as
an implicit opt-in to every namespace.

For a dedicated acceptance environment, install the bridge in its own namespace
and include this fragment in the environment's complete Helm values:

```yaml
admission:
  managedNamespaces:
    - bridge-acceptance
  managedNamespaceSelector: {}
```

Alternatively, select only labeled namespaces:

```yaml
admission:
  managedNamespaceSelector:
    matchLabels:
      bridge-admission: enabled
```

```sh
kubectl create namespace bridge-acceptance
kubectl label namespace bridge-acceptance bridge-admission=enabled
```

The Helm tests cover the non-default namespace and selector-over-list rendering.
The [acceptance runbook](./testing.md#bridge-contract-acceptance) verifies live
webhook behavior. With namespace admission, omit `schedulerName` from the test
Job template and inspect the generated Pod for `slurm-bridge-scheduler`.

Explicitly setting a Pod's `schedulerName` to the configured bridge scheduler
opts it into bridge handling even outside selected namespaces. It does not prove
namespace selection works and is not a namespace security boundary. Admission
validates those explicitly opted-in Pods too, including protection of
bridge-owned job-ID metadata. Selection changes affect new admission requests;
they do not migrate existing workloads to a different scheduler.

Production admission of `boltz2-dev` or `boltz2` belongs in the infrastructure
repository (`ncats/sci-admin`), not a fork chart default. Namespace selection
does not grant Slurm account/QoS permissions or Kubernetes RBAC permissions.

### Sequence Diagram

```mermaid
sequenceDiagram
  autonumber

  participant KAPI as Kubernetes API
  participant SBA as Slurm-Bridge Admission

  KAPI-->>SBA: Watch Pod Create/Update
  opt Pod in managed Namespaces
    SBA->>KAPI: Update `.spec.schedulerName` and Tolerations
    KAPI-->>SBA: Update Response
  end %% opt Pod in managed Namespaces
```

<!-- Links -->

[scheduler]: scheduler.md
