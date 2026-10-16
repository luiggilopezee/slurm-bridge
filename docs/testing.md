# Running slurm-bridge locally

## Table of Contents

<!-- mdformat-toc start --slug=github --no-anchors --maxlevel=6 --minlevel=1 -->

- [Running slurm-bridge locally](#running-slurm-bridge-locally)
  - [Table of Contents](#table-of-contents)
  - [Overview](#overview)
  - [Pre-requisites](#pre-requisites)
  - [Setting up your environment](#setting-up-your-environment)
  - [Installing `slurm-bridge` within your environment](#installing-slurm-bridge-within-your-environment)
  - [Cleaning up](#cleaning-up)
  - [Native Mail Verification](#native-mail-verification)
  - [Bridge Contract Acceptance](#bridge-contract-acceptance)
    - [Preconditions and Admission](#preconditions-and-admission)
    - [Allocation and Lifecycle Checks](#allocation-and-lifecycle-checks)
    - [Optional GPU Case](#optional-gpu-case)
    - [Shared Allocation Cases](#shared-allocation-cases)
    - [Retry, Restart, and Evidence Gates](#retry-restart-and-evidence-gates)

<!-- mdformat-toc end -->

## Overview

You may want to run `slurm-bridge` on a single machine in order to test the
software or familiarize yourself with it prior to installing it on your cluster.
This should only be done for testing and evaluation purposes and should not be
used for production environments.

We have provided a script to do this using [Kind](https://kind.sigs.k8s.io/) and
the
[`hack/kind.sh`](https://github.com/SlinkyProject/slurm-bridge/blob/main/hack/kind.sh)
script.

This document assumes a basic understanding of
[Kubernetes architecture](https://kubernetes.io/docs/concepts/architecture/). It
is highly recommended that those who are unfamiliar with the core concepts of
Kubernetes review the documentation on
[Kubernetes](https://kubernetes.io/docs/concepts/overview/),
[pods](https://kubernetes.io/docs/concepts/workloads/pods/), and
[nodes](https://kubernetes.io/docs/concepts/architecture/nodes/) before getting
started.

## Pre-requisites

- [go 1.17+](https://go.dev/) must be installed on your system

## Setting up your environment

1. Install [Kind](https://kind.sigs.k8s.io/) using `go install`:

   ```bash
   go install sigs.k8s.io/kind@v0.29.0
   ```

   If you get `kind: command not found` when running the next step, you may need
   to add GOPATH to your PATH:

   ```sh
   export GOPATH=$HOME/go
   export PATH=$PATH:$GOROOT/bin:$GOPATH/bin
   ```

1. Confirm that kind is working properly by running the following commands:

   ```bash
   kind create cluster

   kubectl get nodes --all-namespaces

   kind delete cluster
   ```

1. Clone the
   [`slurm-bridge`](https://github.com/SlinkyProject/slurm-bridge/tree/main)
   repo and enter it:

   ```bash
   git clone git@github.com:SlinkyProject/slurm-bridge.git
   cd slurm-bridge
   ```

## Installing `slurm-bridge` within your environment

Provided with `slurm-bridge` is the script `hack/kind.sh` that interfaces with
kind to deploy the `slurm-bridge` helm chart within your local environment.

1. Create your cluster using `hack/kind.sh`:

   ```bash
   hack/kind.sh --core
   ```

1. Familiarize yourself with and use your test environment:

   ```bash
   kubectl get pods --namespace=slurm-bridge
   kubectl get pods --namespace=slurm
   kubectl get pods --namespace=slinky
   ```

At this point, you should have a kind cluster running `slurm-bridge`.

## Cleaning up

`hack/kind.sh` provides a mechanism by which to destroy your test environment.

Run:

```sh
hack/kind.sh --delete
```

To destroy your kind cluster.

## Native Mail Verification

Unit tests cover annotation validation, Job Pod-template precedence, omission of
unset fields, creation-time mail settings, and serialized v0.0.44 event
encoding:

```sh
go test ./internal/utils/slurmjobir ./internal/scheduler/plugins/slurmbridge/slurmcontrol
```

These tests intercept Slurm requests; they do not prove native delivery. Run the
following integration procedure on a dedicated test cluster with this bridge
build and a Slurm version supporting the external-job v0.0.44 API. Do not change
`MailProg`, drain nodes, or force failures on a production cluster for this
test.

1. Configure an executable `MailProg` on every Slurm controller host and an
   approved test recipient/relay. A test capture program may record each
   argument and stdin to a service-writable, access-restricted log instead of
   sending mail. Keep each invocation separate and timestamped; never evaluate
   the arguments as commands. Confirm the configuration using
   `scontrol show config` and test delivery under the Slurm service account.
1. Submit the [mail example](./workload.md#slurm-email-notifications) with
   `BEGIN,END,FAIL,REQUEUE,TIME_LIMIT` and the test recipient. Save the Pod UID
   and its `scheduler.slinky.slurm.net/slurm-jobid` label as soon as it is
   assigned. Check `scontrol show job <job-id>` for the recipient and event
   flags before the workload exits. Inspect `kubectl describe pod <pod>` if
   scheduling fails.
1. For each case below, use a fresh Job name and collect Kubernetes Pod/Job
   status, Slurm controller logs, `sacct -j <job-id> -o JobID,State,ExitCode`,
   and the captured mail subject/recipient/body or relay receipt. Record the
   Slurm version and effective `MailProg` configuration alongside the results.
   Compare native messages to Slurm allocation states, not application outcome.

| Case                 | Action and checks                                                                                                                                                                                                                                             |
| -------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Absent annotations   | Remove both annotations. Verify mail fields are not overridden and cluster defaults are unchanged.                                                                                                                                                            |
| Execution start      | Run the example long enough to observe Slurm `RUNNING`. Verify `BEGIN` delivery and no submission receipt while pending.                                                                                                                                      |
| Kubernetes success   | Let the container exit zero. Verify Kubernetes success and record Slurm cleanup's terminal state; mail must not be interpreted as application success.                                                                                                        |
| Kubernetes failure   | Use `sh -c 'exit 1'` with `backoffLimit: 0`. Verify Kubernetes failure and record the allocation cancellation; do not assume it produces native `FAIL`.                                                                                                       |
| Native Slurm failure | In the isolated cluster, induce a supported allocation failure (for example loss of its test node). Verify a native failure state and requested `FAIL` delivery, separately from container failure.                                                           |
| Pending cancellation | Keep the test partition's resources occupied until the allocation has an ID and remains `PENDING`, then delete the Kubernetes Job. Verify cleanup, no `BEGIN`, and record cancellation mail policy.                                                           |
| Running cancellation | Delete the Job after Slurm `RUNNING`. Verify allocation cleanup and record terminal state and mail.                                                                                                                                                           |
| Timeout              | Set `slurmjob.slinky.slurm.net/timelimit: "1"` on Job metadata and run longer than one minute. Verify Slurm `TIMEOUT` and requested time-limit/terminal mail; account for the cluster's timeout grace period.                                                 |
| Requeue              | Attempt `scontrol requeue <job-id>` for a running test allocation. If supported, verify identity, restart count, retained settings, and `REQUEUE`/subsequent mail. If external jobs are ineligible, record the rejection instead of claiming requeue support. |
| Replacement Pod      | Allow a Kubernetes retry to create a new allocation. Verify the new ID receives template mail settings and its own lifecycle messages.                                                                                                                        |
| Explicit disable     | Repeat with `NONE`. Verify no native notifications, including when a root-owner mail type is overridden by the template.                                                                                                                                      |
| Delivery failure     | Make only the test mail sink return a failure. Verify workload progress and that no additional allocation is created by the bridge because of mail failure.                                                                                                   |

Do not mark the native delivery, cancellation, timeout, or requeue acceptance
checks complete without these live observations. The current bridge
intentionally does not translate Kubernetes success/failure to Slurm completion
exit codes; see the [lifecycle limitation](./workload.md#lifecycle-semantics).
Durable submission notification delivery belongs to
[the separate follow-up](https://github.com/luiggilopezee/slurm-bridge/issues/2).
Allocation submission deduplication across retries/restarts is also a bridge
contract requirement, tested below independently of that notification feature.

## Bridge Contract Acceptance

This is a live, operator-run procedure for issue #6, not a claim that the
current revision passes. **Terminal outcome acceptance is blocked by issue #5.**
Keep observed failures in the evidence; do not weaken expected outcomes to match
the current cancellation behavior. Unit tests and Helm rendering cannot prove
live Slurm accounting or native mail delivery.

### Preconditions and Admission

Use an approved test cluster, dedicated Slurm partition, authorized test
account/QoS/user, and controlled mail recipient. Never occupy production
capacity, restart shared services, or induce failures without operator approval.
Run the shell blocks in Bash with `kubectl`, `jq`, and the Slurm CLI available
for the same environment (or run Slurm commands on its administration host).
Slurm accounting and the [native mail capture](#native-mail-verification) must
be operational. Record the release SHA, chart version, three image digests,
Kubernetes/Slurm versions, enabled API plugins, and device-driver versions.

Configure the bridge's complete environment values with
`admission.managedNamespaces: [bridge-acceptance]` and
`admission.managedNamespaceSelector: {}` as described in
[Admission](./admission.md#namespace-selection-contract). Do not replace a
production namespace list. Provision credentials through infrastructure first.

```sh
set -euo pipefail
kubectl config current-context
export TEST_NAMESPACE=bridge-acceptance
export BRIDGE_NAMESPACE=slurm-bridge
export RUN_ID="accept-$(date -u +%Y%m%d%H%M%S)"
: "${PARTITION:?Set the dedicated Slurm test partition}"
: "${ACCOUNT:?Set an authorized test account}"
: "${QOS:?Set an authorized test QoS}"
: "${SLURM_USER_ID:?Set the authorized Slurm user ID}"
: "${MAIL_USER:?Set the approved test recipient}"
: "${WORKLOAD_IMAGE:?Set an approved image with sh and sleep, preferably by digest}"
kubectl create namespace "$TEST_NAMESPACE"
kubectl version -o json | jq '{clientVersion, serverVersion}'
scontrol --version
kubectl -n "$BRIDGE_NAMESPACE" get deployment -o json |
   jq '[.items[] | {name: .metadata.name, images: [.spec.template.spec.containers[].image]}]'
```

Use a fresh namespace, or have an operator confirm that an existing namespace is
dedicated to this test before omitting the create command. Do not automatically
delete an existing namespace to recover from a name conflict.

The following function creates manifests, not workloads. Policy annotations are
on Job metadata; mail overrides are on the Pod template. `schedulerName` is
deliberately absent so the test exercises namespace admission.

```sh
job_manifest() {
   local name="$1" exit_code="$2" duration="$3"
   kubectl -n "$TEST_NAMESPACE" create job "$name" --image="$WORKLOAD_IMAGE" \
      --dry-run=client -o json -- sh -c "sleep $duration; exit $exit_code" |
      jq --arg name "$name" --arg partition "$PARTITION" --arg account "$ACCOUNT" \
         --arg qos "$QOS" --arg user "$SLURM_USER_ID" --arg mail "$MAIL_USER" \
         --arg run "$RUN_ID" '
         .metadata.labels["bridge-acceptance-run"] = $run |
         .metadata.annotations = {
            "slurmjob.slinky.slurm.net/job-name": $name,
            "slurmjob.slinky.slurm.net/partition": $partition,
            "slurmjob.slinky.slurm.net/account": $account,
            "slurmjob.slinky.slurm.net/qos": $qos,
            "slurmjob.slinky.slurm.net/user-id": $user,
            "slurmjob.slinky.slurm.net/timelimit": "10",
            "slurmjob.slinky.slurm.net/exclusive": "false"
         } |
         .spec.backoffLimit = 0 |
         .spec.template.metadata.labels["bridge-acceptance-run"] = $run |
         .spec.template.metadata.annotations = {
            "slurmjob.slinky.slurm.net/mail-user": $mail,
            "slurmjob.slinky.slurm.net/mail-type": "BEGIN,END,FAIL"
         } |
         .spec.template.spec.restartPolicy = "Never" |
         .spec.template.spec.containers[0].resources = {
            requests: {cpu: "1", memory: "256Mi"},
            limits: {cpu: "1", memory: "256Mi"}
         }'
}
NAME="$RUN_ID-success"
job_manifest "$NAME" 0 120 | kubectl create -f -
kubectl -n "$TEST_NAMESPACE" wait --for=create pod \
   -l "batch.kubernetes.io/job-name=$NAME" --timeout=120s
kubectl -n "$TEST_NAMESPACE" wait \
   --for=jsonpath='{.metadata.labels.scheduler\.slinky\.slurm\.net/slurm-jobid}' \
   pod -l "batch.kubernetes.io/job-name=$NAME" --timeout=180s
POD=$(kubectl -n "$TEST_NAMESPACE" get pod \
   -l "batch.kubernetes.io/job-name=$NAME" -o json | jq -er '.items[0].metadata.name')
kubectl -n "$TEST_NAMESPACE" get pod "$POD" -o json |
   jq -e '.spec.schedulerName == "slurm-bridge-scheduler"'
JOB_ID=$(kubectl -n "$TEST_NAMESPACE" get pod "$POD" -o json |
   jq -er '.metadata.labels["scheduler.slinky.slurm.net/slurm-jobid"] | select(test("^[1-9][0-9]*$"))')
printf 'case=%s pod=%s allocation=%s\n' "$NAME" "$POD" "$JOB_ID"
```

If the configured scheduler name differs, adjust the assertion to the rendered
name. Record the Pod UID as well as its name and label. A timeout is a failure
to investigate, not permission to bypass admission by setting `schedulerName`.

In an isolated admission test, repeat using the label selector example, with a
different namespace in the ignored list. A Pod in the matching labeled namespace
must be mutated; one in an unselected namespace must retain `default-scheduler`.
A Pod explicitly naming the bridge scheduler in that unselected namespace must
still receive bridge handling. Use server-side dry-run Pods for these routing
checks to avoid accidentally running a control workload outside Slurm.

### Allocation and Lifecycle Checks

Capture the allocation while it exists, then its terminal accounting record:

```sh
scontrol show job -dd "$JOB_ID"
kubectl -n "$TEST_NAMESPACE" wait --for=condition=complete "job/$NAME" --timeout=600s
sacct -X -j "$JOB_ID" -P \
   -o JobIDRaw,JobName,State,ExitCode,Partition,Account,QOS,ReqTRES,AllocTRES,NodeList
squeue -h -j "$JOB_ID" -o '%i|%T|%R'
kubectl -n "$TEST_NAMESPACE" get pod "$POD" -o json |
   jq '{uid: .metadata.uid, scheduler: .spec.schedulerName, phase: .status.phase,
          allocation: .metadata.labels["scheduler.slinky.slurm.net/slurm-jobid"],
          images: [.status.containerStatuses[]? | .imageID]}'
```

Compare `NumCPUs`, memory, `ReqTRES`/`AllocTRES`, user, partition, account, and
QoS with the requested 1 CPU/256 MiB and supplied policy values. Explain any
policy-driven rounding or extra allocation; an unexplained mismatch fails the
case. Accounting may lag; use an operator-defined bounded observation window
(for example five minutes), not an immediate empty result as proof of release.
Correlate native mail with the allocation ID and timestamps. Require `BEGIN` at
allocation execution and the terminal events defined by the tested Slurm policy.

Use a fresh name for every row and capture its own Pod UID and allocation ID:

| Case                  | Action                                                                                                                                                     | Required outcome                                                                                                                                 |
| --------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| Success               | `job_manifest "$RUN_ID-success" 0 120` (already submitted above)                                                                                           | Kubernetes Complete, Slurm COMPLETED/zero exit, release, native END; no cancellation caused by cleanup                                           |
| Failure               | Submit `job_manifest "$RUN_ID-failure" 1 120`; wait for Job Failed instead of Complete                                                                     | Distinct failure accounting/nonzero exit and requested native failure notification; release; not relabeled success or explicit user cancellation |
| Pending cancellation  | Operator first makes only the dedicated partition unavailable through controlled test load; submit a fresh long-running case and confirm its ID is PENDING | Delete only that Job; allocation becomes CANCELLED, releases resources, and never sends BEGIN                                                    |
| Running cancellation  | Submit a fresh long-running case; require Slurm RUNNING before deletion                                                                                    | Delete only that Job; allocation becomes CANCELLED and releases resources; capture native cancellation-mail policy                               |
| Cleanup after success | After recording COMPLETED, delete the finished Job and restart controllers in the approved window                                                          | Accounting remains COMPLETED; no new cancellation mail or allocation                                                                             |

For cancellation use
`kubectl -n "$TEST_NAMESPACE" delete job "$NAME" --wait=false` after recording
the current case's identity. Use `squeue -h -j "$JOB_ID" -o '%T'` to confirm the
required PENDING/RUNNING precondition first. Do not call `scancel` as the test
action: that would bypass Kubernetes-to-Slurm cleanup. Do not set TTL cleanup
until evidence has been collected.

### Optional GPU Case

On a node pool with a working NVIDIA device plugin and matching Slurm GRES,
submit a separate case with an approved GPU image. For a different GPU/DRA
driver, substitute its documented resource and probe and record the difference.

```sh
: "${GPU_IMAGE:?Set an approved image containing nvidia-smi}"
NAME="$RUN_ID-gpu"
job_manifest "$NAME" 0 120 |
   jq --arg image "$GPU_IMAGE" '
      .spec.template.spec.containers[0].image = $image |
      .spec.template.spec.containers[0].command = ["sh", "-c", "nvidia-smi -L && sleep 120"] |
      .spec.template.spec.containers[0].resources.requests["nvidia.com/gpu"] = "1" |
      .spec.template.spec.containers[0].resources.limits["nvidia.com/gpu"] = "1"' |
   kubectl create -f -
```

Repeat the identity/allocation checks and inspect workload logs for a visible
GPU. Require one GPU in Slurm allocation TRES as well as the Kubernetes request.
Without hardware/driver support record **SKIP: GPU unavailable**, not PASS.

### Shared Allocation Cases

Use a supported grouping mechanism, not two ordinary Job Pods (which normally
receive separate allocations). This example requires the scheduler-plugins
PodGroup CRD and controller from
[PodGroup coscheduling](./workload.md#podgroup-coscheduling) and at least two
eligible nodes. Run only in the dedicated test environment.

```sh
GROUP="$RUN_ID-mixed"
job_manifest "$GROUP" 0 120 |
   jq '{apiVersion: "scheduling.x-k8s.io/v1alpha1", kind: "PodGroup",
          metadata: .metadata, spec: {minMember: 2}}' |
   kubectl create -f -
group_pod() {
   job_manifest "$1" "$2" "$3" |
      jq --arg group "$GROUP" '
         {apiVersion: "v1", kind: "Pod",
          metadata: (.spec.template.metadata + {name: .metadata.name, namespace: .metadata.namespace}),
          spec: .spec.template.spec} |
         .metadata.labels["scheduling.x-k8s.io/pod-group"] = $group' |
      kubectl create -f -
}
group_pod "$GROUP-first" 1 60
group_pod "$GROUP-second" 0 180
kubectl -n "$TEST_NAMESPACE" get pod -l "scheduling.x-k8s.io/pod-group=$GROUP" \
   -o custom-columns='NAME:.metadata.name,PHASE:.status.phase,ID:.metadata.labels.scheduler\.slinky\.slurm\.net/slurm-jobid'
```

Require both Pods to receive the same non-empty allocation ID. When the first
fails, the allocation must not be released while the second is active. After
both terminate, require failure rather than success/cancellation, native failure
semantics, and release. Repeat with a fresh group and both exit codes zero for
all-success completion. Repeat with a fresh group and delete one running Pod to
verify explicit cancellation remains distinguishable from application failure.
Record the issue #5 policy for surviving group members and verify it; do not
silently infer that policy from whatever the current implementation does.

### Retry, Restart, and Evidence Gates

With approved operator access, restart the scheduler and controllers separately
while a fresh allocation is pending and again while it is running. Use the
[rollout procedure](./config.md#rotation-and-recovery), without rotating the
credential for this case. Capture the Pod UID/job-ID label and all Slurm jobs
matching the unique case name before and after each restart. Require exactly one
allocation for that submission and continued reconciliation. A replacement Pod
is a new identity and may legitimately receive a new allocation.

Also exercise the ambiguous submission window in an isolated fault-injection
environment: delay/drop the Slurm create response after Slurm acceptance but
before the bridge records the ID, then restart/retry the scheduler. Require
recovery of the existing allocation rather than duplicate creation. If this
fault cannot be controlled, mark the case NOT RUN; an ordinary restart does not
prove crash-window deduplication. Repeat controller reconciliation and later
Kubernetes cleanup after terminal completion and verify unchanged accounting.

For each case, retain a sanitized record of:

- Release/source/chart/image identity and environment versions.
- Exact commands, timestamps, case name, Pod UID, allocation IDs, and requested
  versus actual CPU/memory/GPU/partition/account/QoS values.
- Kubernetes terminal conditions, Slurm state/exit code, evidence of resource
  release, and correlated native mail capture or relay receipts.
- Expected outcome, observed outcome, and PASS/FAIL/SKIP/NOT RUN with reason.

Do not export Secrets, JWTs, full environment dumps, unfiltered Helm values, or
raw logs into the acceptance bundle. Redact recipient addresses and other
identity data before sharing. Record only selected fields, review the bundle,
and keep raw mail capture access restricted. Application result indexing and
download validation belong to the downstream application test, not END mail.

After evidence collection, delete only objects bearing this run's label:

```sh
kubectl -n "$TEST_NAMESPACE" delete jobs,pods \
   -l "bridge-acceptance-run=$RUN_ID" --wait=false
```

Delete labeled PodGroups separately if that API was used, and verify every
recorded allocation is terminal/released before removing the test namespace. Do
not mark issue #6 complete until all required cases pass against the release
candidate, including issue #5's corrected terminal semantics.
