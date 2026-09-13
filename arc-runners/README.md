# ARC runner resources

## inotify instance floor

The `configure-inotify` init container raises
`fs.inotify.max_user_instances` to the configured floor before the runner or
dind starts. It reads the current value first and preserves an already higher
value. It changes no file-descriptor limit and no `max_user_watches` value.
A failure to read, write or reach the floor fails initialization visibly;
tests keep their real watchers and are never skipped or retried to hide it.

This is a **node-wide kernel setting for every real UID**, not a pod-local
setting. All ordinary ARC pods use UID 1001 in the node's initial user namespace
on the observed Linux 5.15 workers. Their Go/Node processes, and other pods with
that UID, share one pool of inotify instances. The init container therefore
needs root and privilege (the existing dind container is already privileged).
It has no host mount and does not write `/etc/sysctl*`. The setting lasts until
node reboot or another administrator changes it, and is restored on a new
node's first ARC pod.

`ARC_INOTIFY_MIN_USER_INSTANCES` in the init container's environment is the
operator's control. The default 1024 is eight times the observed 128 ceiling;
it reserves no instances or memory by itself and is a floor, not a concurrency
guarantee for an unbounded scale set. Raise it for larger concurrent workloads.
`0` explicitly disables this node tuning; configure the node elsewhere if using
that option. Coordinate policy changes with node administration: a manual
sysctl write concurrent with this read/write is not an atomic compare-and-set.

Changing the environment back does not lower the setting already written on a
node. Rollback is to disable this initializer and have the node owner restore
its previous policy; do not lower a live node's ceiling during active jobs.

## Evidence for SocialGouv/iterion#1198

On 2026-09-13, two independent Iterion PR jobs had previously failed to
initialize a watcher with `EMFILE` (`too many open files`). A diagnostic CI job
on ARC reported `real_uid=1001`, `RLIMIT_NOFILE=1048576`,
`max_user_instances=128`, `max_user_watches=228254` and an identity user map.

A read-only examination of `worker-nodepool-node-e6dab0` confirmed kernel
5.15.0-134-generic and that same 128 ceiling. At the time of inspection no
runner process remained, so the historical peak was not measured. We then
used a bounded one-shot job under the separately checked, unused UID 2147480000:

```json
{"uid":2147480000,"max_user_instances":128,"nofile":[1048576,1048576],"child":{"held":128,"errno":24},"other_process_inotify_errno":24,"ordinary_fd":"open_ok"}
```

One process held 128 instances and the next initialization failed with errno
24. A second process under the same UID also got `EMFILE`, while `/dev/null`
still opened. Closing the first process's descriptors immediately restored
capacity. The job added no watches, changed no sysctl, used no runner UID and
was deleted after completion. This reproduces the per-UID resource boundary
on the real worker; it does not invent a measured historical descriptor peak.
The reproducible job and failure-time watcher diagnostics are in Iterion
PR #1200.

## Validation and rollout

Local tests execute the shipped shell body against a temporary file, never
against the developer's sysctl. They cover the observed 128→1024 change, an
already higher value, a custom floor, explicit disable, invalid/overflow values and missing sysctl. They also check
the effective init order in the rendered runner PodSpec:

```sh
helm dependency build arc-runners
python3 arc-runners/tests/inotify_init.py
helm lint arc-runners
helm template arc-runners arc-runners --namespace arc-runners
```

Before approval, validate the rendered AutoscalingRunnerSet and its PodSpec
with server-side dry-run. For the complete chart, the following also inspects
unrelated existing resources and may report their field ownership conflicts:

```sh
helm template arc-runners arc-runners --namespace arc-runners \
  | kubectl --context ovh-dev apply --server-side \
      --field-manager=argocd-controller --dry-run=server -f -
```

The changed AutoscalingRunnerSet and extracted runner PodSpec were separately
accepted in server dry-run on ovh-dev. The full chart dry-run reported an
existing RoleBinding `subjects` ownership conflict between ArgoCD managers;
do not use `--force-conflicts` to hide that unrelated drift.

After approved merge/sync, inspect the initializer log on a new ARC pod and
verify the floor from that pod. Existing pods are not restarted to make a test
pass. Repeat the representative concurrent CI workload with watcher diagnostics
present and record its result on #1198 before closing the ticket. The prepared
configuration alone is not evidence that the node setting has been applied.
