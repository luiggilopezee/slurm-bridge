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
Durable submission delivery and restart-deduplication tests belong to
[the separate follow-up](https://github.com/luiggilopezee/slurm-bridge/issues/2).
