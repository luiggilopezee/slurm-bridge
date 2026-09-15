# Workloads

## Table of Contents

<!-- mdformat-toc start --slug=github --no-anchors --maxlevel=6 --minlevel=1 -->

- [Workloads](#workloads)
  - [Table of Contents](#table-of-contents)
  - [Overview](#overview)
  - [Using the `slurm-bridge` Scheduler](#using-the-slurm-bridge-scheduler)
  - [CPU DRA](#cpu-dra)
  - [Annotations](#annotations)
    - [Resolution rules](#resolution-rules)
    - [Supported Slurm job annotations](#supported-slurm-job-annotations)
    - [Scheduler-managed Pod metadata](#scheduler-managed-pod-metadata)
  - [Pod grouping](#pod-grouping)
    - [Other controller owners](#other-controller-owners)
  - [PodGroup (1.36+)](#podgroup-136)
  - [Slurm Email Notifications](#slurm-email-notifications)
    - [Annotation Contract](#annotation-contract)
    - [Delivery and Trust](#delivery-and-trust)
    - [Lifecycle Semantics](#lifecycle-semantics)
    - [Confirmed Submission Follow-up](#confirmed-submission-follow-up)
  - [JobSets](#jobsets)
  - [PodGroup coscheduling](#podgroup-coscheduling)
  - [LeaderWorkerSet](#leaderworkerset)

<!-- mdformat-toc end -->

## Overview

In Slurm, all workloads are represented by jobs. In `slurm-bridge`, however,
there are a number of forms that workloads can take. While workloads can still
be submitted as a Slurm job, `slurm-bridge` also enables users to submit
workloads through Kubernetes. Most workloads that can be submitted to
`slurm-bridge` from within Kubernetes are represented by an existing Kubernetes
batch workload primitive.

At this time, `slurm-bridge` has scheduling support for [Jobs],
[JobSets](#jobsets), [Pods], [PodGroup (1.36+)](#podgroup-136)
(`scheduling.k8s.io/v1alpha2`), [PodGroup coscheduling](#podgroup-coscheduling)
(scheduler-plugins), and [LeaderWorkerSets]. If your workload requires or
benefits from co-scheduled pod launch (e.g. MPI, multi-node), prefer
[PodGroup (1.36+)](#podgroup-136) on Kubernetes **1.36+** or
[PodGroup coscheduling](#podgroup-coscheduling) on older clusters.

## Using the `slurm-bridge` Scheduler

`slurm-bridge` uses an
[admission controller](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/)
to control which resources are scheduled using the `slurm-bridge-scheduler`. The
`slurm-bridge-scheduler` is designed as a non-primary scheduler and is not
intended to replace the default
[kube-scheduler](https://kubernetes.io/docs/concepts/architecture/#kube-scheduler).
The `slurm-bridge` admission controller only schedules pods that request
`slurm-bridge` as their scheduler or are in a configured namespace. By default,
the `slurm-bridge` admission controller is configured to automatically use
`slurm-bridge` as the scheduler for all pods in the configured namespaces.

Alternatively, a pod can specify `Pod.Spec.schedulerName=slurm-bridge-scheduler`
from any namespace to indicate that it should be scheduler using the
`slurm-bridge-scheduler`.

Please review [`slurm-bridge` admission controller](./admission.md) to learn
more.

## CPU DRA

Native `cpu` requests do not activate the CPU DRA driver. To request CPUs from
the `dra.cpu` DeviceClass, specify its extended resource explicitly:

```yaml
resources:
  requests:
    deviceclass.resource.kubernetes.io/dra.cpu: "2"
  limits:
    deviceclass.resource.kubernetes.io/dra.cpu: "2"
```

The extended resource quantity is used as the Slurm CPU count. A Pod that
requests this resource cannot also specify native `cpu` requests or limits.

CPU DRA constrains the container to Slurm's allocated CPU set. The CPU driver
also removes DRA-allocated CPUs from the shared CPU sets of running native
containers. Native CPU requests still reserve capacity in Slurm, but native
containers share all CPUs not claimed through DRA; Slurm's native CPU IDs do not
define their container CPU sets.

## Annotations

Users can influence how `slurm-bridge` represents their Kubernetes workload in
Slurm by adding `slurmjob.slinky.slurm.net/*` annotations to the annotation
source identified in [Pod grouping](#pod-grouping). These annotations configure
the Slurm external job; they are separate from the
[scheduler-managed metadata](#scheduler-managed-pod-metadata).

Example "pause" bare pod to illustrate annotations:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: pause
  # Slurm job annotations on this Pod
  annotations:
    slurmjob.slinky.slurm.net/timelimit: "5"
    slurmjob.slinky.slurm.net/account: foo
spec:
  schedulerName: slurm-bridge-scheduler
  containers:
    - name: pause
      image: registry.k8s.io/pause:3.6
      resources:
        limits:
          cpu: "1"
          memory: 100Mi
```

Example "sleep" Job to illustrate annotations:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: sleep
  # Slurm job annotations on the Job, not spec.template.metadata
  annotations:
    slurmjob.slinky.slurm.net/timelimit: "5"
    slurmjob.slinky.slurm.net/account: foo
spec:
  template:
    spec:
      schedulerName: slurm-bridge-scheduler
      restartPolicy: Never
      containers:
        - name: sleep
          image: busybox:stable
          command: [sh, -c, sleep 3]
          resources:
            limits:
              cpu: "1"
              memory: 100Mi
```

### Resolution rules

Slurm parameters are resolved from lowest to highest precedence: **scheduler or
Slurm defaults -> values derived from the workload and Pods -> Slurm job
annotations**. For built-in PodGroups, annotation precedence is **PodGroup ->
selected controller (Job or JobSet) -> Workload**.

Keys that do not conflict are combined.

The two [mail annotations](#slurm-email-notifications) additionally accept
per-key overrides on the scheduling Pod, including Job Pod templates.

### Supported Slurm job annotations

| Purpose                 | Annotation suffixes                                                                   |
| ----------------------- | ------------------------------------------------------------------------------------- |
| Identity and accounting | `account`, `group-id`, `user-id`, `wckey`                                             |
| Scheduling policy       | `constraints`, `exclusive`, `licenses`, `partition`, `priority`, `qos`, `reservation` |
| Resources               | `cpu-per-task`, `gres`, `max-nodes`, `mem-per-node`, `min-nodes`                      |
| Naming and duration     | `job-name`, `timelimit`                                                               |
| Native mail             | `mail-user`, `mail-type`                                                              |

Prefix every suffix with `slurmjob.slinky.slurm.net/`; for example,
`slurmjob.slinky.slurm.net/account`. Node counts, priority, and `timelimit` are
base-10 integers; `timelimit` is measured in minutes. CPU and memory accept
Kubernetes quantities. `exclusive: "false"` requests non-exclusive placement;
exclusive placement is the default.

Annotations can update a Slurm job while it is pending. Slurm validates each
change. If it rejects an update, the previous Slurm job value remains in effect.
Once Slurm allocates the job, treat its annotations and Pod membership as fixed.
Mail settings are an exception: the bridge sends them only at creation.

### Scheduler-managed Pod metadata

Slurm-bridge writes the following metadata to Pods as it creates and allocates
the external job:

| Metadata                                 | Type       | Meaning                                                                                                |
| ---------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------ |
| `scheduler.slinky.slurm.net/slurm-jobid` | Label      | Slurm job ID associated with the Pod. All Pods represented by one external job receive the same value. |
| `slinky.slurm.net/slurm-node`            | Annotation | Kubernetes node selected from the Slurm allocation for this Pod.                                       |

These keys are owned by slurm-bridge. Users cannot set them when creating a
managed Pod or change them after that Pod is running.

## Pod grouping

Slurm-bridge turns each group of Kubernetes Pods into one Slurm external job:

| Workload                  | Pods in one external job                                 | Where to put annotations                      |
| ------------------------- | -------------------------------------------------------- | --------------------------------------------- |
| Pod                       | That Pod                                                 | Pod                                           |
| Job or JobSet             | One Pod                                                  | Job or JobSet                                 |
| Built-in PodGroup         | Pods with the same `spec.schedulingGroup.podGroupName`   | PodGroup, selected Job or JobSet, or Workload |
| PodGroup coscheduling     | Pods with the same `scheduling.x-k8s.io/pod-group` label | PodGroup                                      |
| LeaderWorkerSet           | One LeaderWorkerSet group                                | LeaderWorkerSet                               |
| Other readable controller | One Pod                                                  | Highest readable controller                   |

Slurm-bridge first follows controller owner references toward the root object.
It selects the first applicable grouping mechanism in this order: **built-in
PodGroup -> PodGroup coscheduling -> highest recognized workload type in the
owner chain -> the Pod itself**. When no recognized workload exists, the highest
readable controller remains the annotation source for the per-Pod external job.

For Jobs, JobSets, and LeaderWorkerSets, annotations belong on the top-level
workload object, not its Pod template or generated child objects, except for the
mail overrides described below.

For grouped workloads (excluding the mail-only Pod overrides):

- **JobSet:** JobSet annotations apply to every per-Pod external job.
  Annotations on generated Jobs or Pods are ignored.
- **LeaderWorkerSet:** LeaderWorkerSet annotations apply to every group external
  job. Pod annotations are ignored.
- **Built-in PodGroup:** annotations are merged from the PodGroup, one selected
  controller (Job or JobSet), and Workload. If a Job belongs to a JobSet, the
  JobSet is selected and the intermediate Job's annotations are ignored. Pod
  annotations are also ignored.
- **PodGroup coscheduling:** only PodGroup annotations apply to the group.
  Owning Job and Pod annotations are ignored.

The built-in Workload API is therefore different from the other grouped
workloads: it merges scoped annotation sources instead of reading one top-level
source.

### Other controller owners

The Pod's direct controller owner must exist and be readable by the slurm-bridge
scheduler. While following the owner chain, slurm-bridge remembers the highest
recognized workload type. Higher readable controllers do not replace that
workload as the scheduling root. If no recognized workload is found,
slurm-bridge schedules each Pod as a separate external job and reads annotations
from the highest readable controller. Annotations on lower controllers and the
Pod are ignored.

If RBAC forbids access to a higher, unsupported controller, traversal stops and
slurm-bridge uses the highest recognized workload already found, or otherwise
the highest readable controller.

Slurm-bridge does not fall back when access to a supported workload type is
forbidden; that indicates missing scheduler RBAC. It also does not fall back
when an owner object is missing, its API kind is not served, or the API request
fails for another reason. These conditions indicate a broken owner chain or a
potentially transient cluster error, so scheduling fails instead.

The default scheduler RBAC can read ReplicaSets and StatefulSets, but not
Deployments, DaemonSets, or arbitrary custom controllers. For example, owner
resolution for `Deployment -> ReplicaSet -> Pod` stops at the ReplicaSet if the
scheduler cannot read the Deployment, and the Pod is still scheduled. If the
ReplicaSet itself cannot be retrieved, scheduling fails because no controller in
the chain was successfully resolved.

## PodGroup (1.36+)

PodGroup is a built-in Kubernetes API introduced in **1.36**. This section
applies to clusters running **1.36+** with the **`GenericWorkload`** feature
gate and **`scheduling.k8s.io/v1alpha2`** API enabled (see
[`hack/kind.yaml`](../hack/kind.yaml) and `make demo-examples`). After
slurm-bridge assigns nodes to the gang, PodGroup `STATUS` becomes **Scheduled**
(`PodGroupScheduled=True`); it is not tied to Job completion.

A [**Workload**][workload-api] defines immutable **`podGroupTemplates`** (gang
or basic scheduling). Workload controllers create runtime **`PodGroup`** objects
from those templates. Pods opt in with **`spec.schedulingGroup.podGroupName`**
pointing at their **`PodGroup`**. `slurm-bridge` reads the PodGroup, groups pods
by scheduling group, and applies the same external-job flow as other
co-scheduled workload types.

Example manifests (see also
[`hack/examples/workload/`](../hack/examples/workload/)):

```yaml
apiVersion: scheduling.k8s.io/v1alpha2
kind: Workload
metadata:
  name: training-workload
  annotations:
    slurmjob.slinky.slurm.net/job-name: training-job
    slurmjob.slinky.slurm.net/timelimit: "5"
spec:
  controllerRef:
    apiGroup: batch
    kind: Job
    name: training-job
  podGroupTemplates:
    - name: workers
      schedulingPolicy:
        gang:
          minCount: 2
---
apiVersion: scheduling.k8s.io/v1alpha2
kind: PodGroup
metadata:
  name: training-job-workers
spec:
  podGroupTemplateRef:
    workload:
      workloadName: training-workload
      podGroupTemplateName: workers
  schedulingPolicy:
    gang:
      minCount: 2
---
apiVersion: batch/v1
kind: Job
metadata:
  name: training-job
spec:
  template:
    spec:
      schedulerName: slurm-bridge-scheduler
      schedulingGroup:
        podGroupName: training-job-workers
```

Ref: [Workload API][workload-api]

To override Slurm submission parameters, add optional
`slurmjob.slinky.slurm.net/*` annotations on the **Workload**, selected
controller (**Job** or **JobSet**), or runtime **PodGroup**. On conflict,
**Workload** > **selected controller** > **PodGroup**. Without them, the Slurm
job name defaults to the **PodGroup object name** (not the Workload name) and
the partition defaults to the scheduler configuration. See
[Annotations](#annotations) for the full key list.

If multiple layers set `slurmjob.slinky.slurm.net/job-name`, annotations are
applied **PodGroup -> selected controller -> Workload**, so the Workload wins.
In the example above, the PodGroup is named `training-job-workers` but the
Workload sets `slurmjob.slinky.slurm.net/job-name: training-job`, so Slurm
receives **`training-job`**. A PodGroup cannot override a `job-name` set on its
Workload or selected controller. For per-gang names, omit `job-name` from
broader sources and set it on each **PodGroup** instead.

A Workload may define several `podGroupTemplates`, each producing a runtime
PodGroup. Workload-level identifiers such as `job-name` then apply to **every**
PodGroup under that Workload. Each gang still submits a separate Slurm external
job (distinct job ID on the pods), but all share the same Slurm job **name** in
`squeue`. Use the Workload for **shared** parameters (partition, account, QOS,
time limit) and **PodGroup** for per-gang identifiers.

## Slurm Email Notifications

Native Slurm mail reports the lifecycle of the external Slurm allocation. For
Kubernetes Jobs, put the optional mail annotations on the Pod template:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: mail-example
spec:
  backoffLimit: 0
  template:
    metadata:
      annotations:
        slurmjob.slinky.slurm.net/mail-user: "user@nih.gov"
        slurmjob.slinky.slurm.net/mail-type: "END,FAIL"
    spec:
      schedulerName: slurm-bridge-scheduler
      restartPolicy: Never
      containers:
        - name: work
          image: busybox:1.37
          command: ["sh", "-c", "sleep 30"]
          resources:
            requests:
              cpu: "1"
              memory: 64Mi
```

For a bare Pod, use `metadata.annotations`. Applications such as Boltz2 should
pass their authorized per-job notification address into the manifest builder and
set `spec.template.metadata.annotations` before creating the Job. Add `BEGIN` to
request execution-start mail; it is not a submission receipt.

### Annotation Contract

- `slurmjob.slinky.slurm.net/mail-user`: one bare email address, such as
  `user+job@nih.gov`. Empty values, display names, address lists, local
  usernames, surrounding whitespace, and line breaks are rejected.
- `slurmjob.slinky.slurm.net/mail-type`: comma-separated, case-sensitive event
  names. Supported values are `NONE`, `BEGIN`, `END`, `FAIL`, `REQUEUE`, `ALL`,
  `INVALID_DEPEND`, `STAGE_OUT`, `TIME_LIMIT`, `TIME_LIMIT_90`, `TIME_LIMIT_80`,
  `TIME_LIMIT_50`, and `ARRAY_TASKS`. Whitespace around events is trimmed and
  duplicates are removed. Empty or unknown events (including `SUBMIT`) are
  rejected with a diagnostic identifying the annotation.
- `NONE` explicitly disables mail and cannot be combined with other events.
  `ALL` expands to `BEGIN,END,FAIL,REQUEUE,STAGE_OUT,INVALID_DEPEND`; time-limit
  warnings and `ARRAY_TASKS` must be requested separately. This includes invalid
  dependencies, matching `parse_mail_type()` in the
  [Slurm 25.11 parser](https://github.com/SchedMD/slurm/blob/slurm-25-11-0-1/src/common/proc_args.c).
- For these two annotations only, the scheduling Pod takes precedence over the
  root owner, independently for each key. For built-in PodGroups, the normal
  PodGroup/controller/Workload sources are applied first, followed by mail-only
  Pod overrides. Other annotations retain their existing resolution rules.
  Invalid annotation values prevent Slurm submission and appear in scheduler
  diagnostics; Kubernetes API acceptance is not validation. Built-in PodGroup
  sources are validated in order, even if a later source overrides them.
- Missing keys remain unset in the Slurm request. A recipient alone does not
  enable events; event types alone leave recipient selection to Slurm defaults.
  Set both for deterministic application routing. With neither annotation,
  existing behavior is unchanged.

The bridge sets `mail_user` and `mail_type` on the creation request using the
pinned Slurm client's v0.0.44 API. `NONE` becomes an explicit empty JSON array;
`TIME_LIMIT[_90|_80|_50]` becomes `TIME=100%`, `TIME=90%`, `TIME=80%`, or
`TIME=50%`, and `INVALID_DEPEND` becomes `INVALID_DEPENDENCY`. No post-creation
mail patch is needed, avoiding a race with short allocations.

### Delivery and Trust

Administrators must configure Slurm's
[`MailProg`](https://slurm.schedmd.com/slurm.conf.html#OPT_MailProg) on the
Slurm controller hosts, ensure it is executable by the Slurm service account,
and configure the institutional mail relay, sender policy, and delivery
monitoring. The application API, bridge, and worker Pods do not need SMTP
credentials. The bridge passes the recipient as structured request data, never a
shell command. Custom `MailProg` implementations must also treat their arguments
as data.

Annotation authors are trusted to choose recipients. Syntax validation does not
prove ownership or authorize delivery. Applications should use an IdP-verified
address or an explicitly authorized alternate address; a domain allowlist alone
is insufficient. Restrict annotation authors through Kubernetes RBAC/admission
policy and apply relay abuse controls. Addresses are visible to readers of Pod,
Job, and Slurm metadata, so account for that access and retention boundary.

Native delivery is handled by Slurm and the relay, outside bridge
reconciliation. The bridge does not retry email or resubmit allocations when
delivery fails. See the
[mail verification procedure](./testing.md#native-mail-verification) before
enabling notifications for users.

### Lifecycle Semantics

| Event                                            | Meaning                                                                   |
| ------------------------------------------------ | ------------------------------------------------------------------------- |
| Kubernetes accepts a Job                         | Application acceptance only; no Slurm confirmation mail is emitted.       |
| Slurm assigns an allocation ID, possibly pending | Confirmed Slurm acceptance; no standard `SUBMIT` mail type exists.        |
| `BEGIN`                                          | Slurm allocation starts, not necessarily application readiness.           |
| `END` / `FAIL`                                   | Slurm allocation terminates or fails, not Kubernetes Job success/failure. |

**Current limitation:** when all Pods associated with an allocation become
terminal, the Pod controller deletes/cancels that Slurm allocation. It uses the
same cleanup operation for Kubernetes `Succeeded` and `Failed` Pods, and does
not translate container exit codes into Slurm application completion status.
Native mail can therefore report cancellation after successful Kubernetes work,
and a Kubernetes failure need not produce Slurm `FAILED` or `FAIL` mail. Do not
relabel allocation cleanup as successful application completion. Use Kubernetes
Job conditions or application state for application outcome notifications.

Cancellation while pending or running and time-limit expiry follow Slurm's state
and mail policy. Verify actual terminal states and delivery on the deployed
Slurm version, including external-job behavior. Slurm completion also does not
imply that an application has finished indexing or publishing results.

Mail settings are fixed when an allocation is created; bridge allocation updates
do not overwrite them. For grouped workloads, use identical settings on all Pods
sharing an allocation: the Pod that triggers creation selects its mail settings.
If Slurm permits a requeue, native `REQUEUE` and subsequent lifecycle mail
belong to that allocation; the bridge does not deduplicate Slurm mail. A
replacement Pod or Job that creates a new allocation gets its own mail
lifecycle, even if it represents a retry of the same application job. The
annotations do not enable requeue support for otherwise ineligible external
jobs.

### Confirmed Submission Follow-up

A durable confirmed-submission integration is tracked separately in
[issue #2](https://github.com/luiggilopezee/slurm-bridge/issues/2); it is not
implemented by the native-mail annotations. The event must follow confirmed
Slurm creation and durable job-ID recording, use persistent identity and an
outbox or equivalent durable queue, and support asynchronous retries and
consumer deduplication across reconciliation and controller restarts. Kubernetes
Events alone are not a durable delivery queue. Delivery failures must neither
fail valid workloads nor create duplicate Slurm allocations. Requeues of an
existing allocation must retain submission identity; new allocations for
replacement Pods must receive new identities. Crash recovery and restart tests
are required before this integration can serve as a submission receipt.

## JobSets

This section assumes [JobSets] is installed.

JobSet pods are scheduled on a per-pod basis. The JobSet controller is
responsible for managing the JobSet status and other Pod interactions once
marked as completed.

## PodGroup coscheduling

This is **not** the same API as [PodGroup (1.36+)](#podgroup-136) above. It uses
the **scheduler-plugins** CRD `scheduling.x-k8s.io/v1alpha1` and requires
installing on clusters **before 1.36** (or where the built-in PodGroup API is
unavailable) the [PodGroup coscheduling CRD][podgroups-crd] plus the out-of-tree
CoScheduling controller:

```sh
helm install --repo https://scheduler-plugins.sigs.k8s.io scheduler-plugins scheduler-plugins \
  --namespace scheduler-plugins --create-namespace \
  --set 'plugins.enabled={CoScheduling}' --set 'scheduler.replicaCount=0'
```

Pods join the group via the label `scheduling.x-k8s.io/pod-group` (see
[`hack/examples/podgroup-coscheduling/`](../hack/examples/podgroup-coscheduling/)).
Gang size is `spec.minMember` on the PodGroup object.

|                 | PodGroup (1.36+)                      | PodGroup coscheduling                 |
| --------------- | ------------------------------------- | ------------------------------------- |
| API group       | `scheduling.k8s.io/v1alpha2`          | `scheduling.x-k8s.io/v1alpha1`        |
| Install         | Feature gate + runtime config         | CRD + helm chart                      |
| Pod association | `spec.schedulingGroup.podGroupName`   | Label `scheduling.x-k8s.io/pod-group` |
| Gang field      | `spec.schedulingPolicy.gang.minCount` | `spec.minMember`                      |

Both paths are supported by `slurm-bridge` independently.

## LeaderWorkerSet

This section assumes [LeaderWorkerSet][leaderworkersets] is installed.

LeaderWorkerSet groups will be co-scheduled so pods of each group will be
guaranteed to launch together.

> [!NOTE]
> Topology-aware placement is not supported yet, so some features of
> LeaderWorkerSet may not behave as expected.

<!-- Links -->

[jobs]: https://kubernetes.io/docs/concepts/workloads/controllers/job/
[jobsets]: https://jobset.sigs.k8s.io/
[leaderworkersets]: https://lws.sigs.k8s.io/
[podgroups-crd]: https://github.com/kubernetes-sigs/scheduler-plugins/blob/master/config/crd/bases/scheduling.x-k8s.io_podgroups.yaml
[pods]: https://kubernetes.io/docs/concepts/workloads/pods/
[workload-api]: https://kubernetes.io/docs/concepts/workloads/workload-api/
