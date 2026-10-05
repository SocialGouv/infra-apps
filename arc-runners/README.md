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

Requires Python 3.8+ and Helm 4.1.1. Install the pinned Python dependency
in an isolated virtual environment using the commands below.

Local tests execute the shipped shell body against a temporary file, never
against the developer's sysctl. They cover the observed 128→1024 change, an
already higher value, a custom floor, explicit disable, invalid/overflow values and missing sysctl. They also check
the effective init order in the rendered runner PodSpec:

```sh
python3 -m venv /tmp/arc-test-venv
/tmp/arc-test-venv/bin/python -m pip install -r arc-runners/tests/requirements.txt
helm dependency build arc-runners
/tmp/arc-test-venv/bin/python arc-runners/tests/inotify_init.py
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

## Revi and reproducible checks — 2026-09-13

[Run 01a09c65-6cc3-7604-be22-5c89de14120d](https://iterion.cloud/runs/01a09c65-6cc3-7604-be22-5c89de14120d)
reviewed `09322f0f0d530ae23246ce22635040ac752ed37d` and reported R1d39ee:
PyYAML was undeclared and the check had no CI entry point. The correction pins
PyYAML 6.0.3, documents a clean virtual environment, and adds the
`ARC runner configuration` workflow with pinned actions and Helm. It needs no
cluster credential and never writes a real sysctl.

The repository Actions API reported `enabled: false` on 2026-09-13. The workflow
is prepared, but automatic execution also requires the repository/organization
owner to enable Actions; do not report a CI pass until that run exists. Local
validation uses the same pinned dependency in a fresh virtual environment.
Billy's Claude weekly quota is blocked until 2026-09-15 21:00 UTC, so R1d39ee was
corrected directly and no Billy run was launched for this PR.

## Docker daemon startup gate — SocialGouv/iterion#981

The runner's `Initialize containers` phase happens before any workflow step.
Waiting inside a CI `run:` step is too late for a job with `services:`. The
observed service job failed eight seconds after pod startup, while another
job on the same image could reach Docker after 76 seconds. Group membership
was not the cause of that incident.

`dind` now runs as a native sidecar in `initContainers`, after the externals
copy and before the runner. Its `restartPolicy: Always` keeps the daemon alive;
kubelet starts the runner only after `startupProbe` succeeds. The probe runs
`docker --host=unix:///run/docker/docker.sock info`, against the same endpoint
as the runner. Merely seeing a socket is insufficient. Five-second probe
period/timeout and 24 failures before restart are explicit operator settings
in values.yaml. A daemon that cannot start keeps the runner in initialization,
without accepting a GitHub job it cannot serve.

This requires Kubernetes 1.29+ with `SidecarContainers` enabled (on by default
there); ovh-dev was read-only verified at 1.31.6. See the
[Kubernetes sidecar startup contract](https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/#sidecar-containers-and-pod-lifecycle).
The runner command, image, Docker GID, socket mounts and privileges retain their
existing configuration. Native sidecar termination also keeps dind alive until
the runner has stopped.

Validate with `python arc-runners/tests/dind_startup.py` in the same pinned
virtual environment as the inotify checks, then server-dry-run the rendered
AutoscalingRunnerSet and extracted PodSpec. After approved activation, record
a real ARC job with `services: mongo` passing container initialization before
moving required jobs. The compiler image for `race` and the unexplained
`cloud-e2e` failure remain separate parts of #981; this gate alone does not
justify moving or closing all three jobs.

## Runner image — SocialGouv/iterion#981

`runner` and `init-dind-externals` run `ghcr.io/socialgouv/iterion-ci-runner`:
upstream's runner image plus gcc and the libc headers Go's race detector
needs. SocialGouv/iterion builds it from `ci/arc-runner/Dockerfile` (workflow
`ARC CI runner image`), proves cgo under `-race` as UID 1001, and publishes
only validated `main` builds, one immutable version `1.<run>.<attempt>` each —
never `latest`. The package is public; the pods pull it anonymously, as they
pull upstream's.

values.yaml pins the version **and** its digest. Resolve the digest
anonymously from the registry right before writing it, and pin what the tag
resolves to at that moment:

```sh
tok=$(curl -fsS "https://ghcr.io/token?scope=repository:socialgouv/iterion-ci-runner:pull" | jq -r .token)
curl -fsSI -H "Authorization: Bearer $tok" \
  -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json' \
  https://ghcr.io/v2/socialgouv/iterion-ci-runner/manifests/<version> | grep -i docker-content-digest
```

Both containers carry the same image: the init copies `/home/runner/externals`
out of the distribution the runner then executes. `configure-inotify` runs
`/bin/sh` only and keeps upstream's image. `python arc-runners/tests/runner_image.py`
holds the pin and the alignment on the rendered PodSpec. Renovate updates the
pinned version and digest when iterion publishes a new build (its Dockerfile
follows upstream's runner through Renovate as well); move `runner` and
`init-dind-externals` together, always.

Rollback is the previous pin on both containers — or upstream's
`ghcr.io/actions/actions-runner:<version>` on both, which loses `-race` (cgo)
on ARC and nothing else.

## Privileged images are pinned by digest

`dind` and `configure-inotify` run privileged: each owns the node it lands on.
OVH forces `AlwaysPullImages`, so a floating tag is resolved again by every
pod at its own start, and an upstream push would run privileged, unreviewed,
on the next job — the merge queue's required checks included. Both carry a
version **and** its digest, and `python arc-runners/tests/privileged_images.py`
refuses any privileged container of the rendered PodSpec that does not.

Bump `dind` by resolving the tag's digest anonymously right before writing it:

```sh
tok=$(curl -fsS "https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/docker:pull" | jq -r .token)
curl -fsSI -H "Authorization: Bearer $tok" \
  -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json' \
  https://registry-1.docker.io/v2/library/docker/manifests/<version>-dind | grep -i docker-content-digest
```

`configure-inotify` keeps upstream's runner image at the version and digest the
iterion CI image is built `FROM` (`ci/arc-runner/Dockerfile` in
SocialGouv/iterion); the ghcr command above resolves it with
`repository:actions/actions-runner:pull`.

## CPU and memory requests

Measured on SocialGouv/iterion's CI on 2026-09-29, 14:56-15:41Z: a sample
every 30 s of `kubectl top pod --containers`, 39 runner pods, the runner
container's usage per job.

| Job | Mean CPU per pod (median / max) | Peak CPU | Peak memory |
|---|---|---|---|
| test | 3.1 / 4.7 cores | 11.6 | 4.6 GiB |
| golangci | 2.7 / 3.7 | 11.7 | 5.3 GiB |
| fmt-check | 1.5 / 1.8 | 4.9 | 1.3 GiB |
| docs-build | 1.1 / 1.2 | 1.9 | 2.6 GiB |
| nats-conformance | 1.1 / 1.5 | 7.1 | 2.1 GiB |
| desktop-vet-cross | 0.7 / 1.3 | 3.7 | 2.9 GiB |
| govulncheck | 0.6 / 0.8 | 2.1 | 0.8 GiB |
| vendor-check | 0.3 / 0.4 | 0.6 | 0.6 GiB |

`dind` used 25-32 MiB idle and 68 MiB with a NATS broker inside. At the
busiest sample, 11 runner pods used 21.7 cores and 15 GiB together — 2 cores
per pod, where the requests counted 1 (500m for the runner, 500m for dind).

So the runner requests **2 CPU and 4 GiB** — the per-pod average at the
peak, and the memory of the heavy jobs — `dind` 100m / 512Mi (a job's
containers run in its cgroup: a mongod for iterion's `mongo-conformance`),
and the externals copy 100m / 128Mi (it runs before the other two, never
beside them). A pod requests 2.1 CPU and 4.5 GiB; when the nodes are full,
pods wait and the worker pool's autoscaler adds nodes. There is no limit: a
burst uses the node's idle cores.

**Raised to 4 CPU / 8 GiB on 2026-10-05**, with the dedicated CI nodepool
(below): iterion's `race` job — the merge queue's floor — peaks at
6.2-8.6 GiB against the 4 GiB request (`-race` multiplies memory 5-10x;
measured by the iterion queue-speed work, ADR-105 era). The scheduler now
reserves what the critical-path job actually uses instead of borrowing the
shared pool's spare capacity; one runner pod per ~4 vCPU / 16 Gi node, so
the pod's bursts still have the node to themselves.

## Dedicated CI nodepool (`ci`) on ovh-dev

Runner pods pin `nodeSelector: nodepool=ci` and tolerate
`pool=ci:NoSchedule` — the same taint convention as ovh-prod's prod-build
pool. This keeps the privileged dind and untrusted CI execution off the
app / control-plane nodes, and the platform's bursts off the merge queue's
critical path (2026-09-29 peak: 39 runner pods sharing the default worker
pool with the platform).

Console-side (OVH), before the values that pin it are synced:

1. ovh-dev -> Node pools -> create `ci`: flavor ~4 vCPU / 16 Gi, min 0,
   max ~12, autoscaling on.
2. Set the pool's labels `nodepool=ci` and taint `pool=ci:NoSchedule`
   (mirrors prod-build on ovh-prod).
3. Then sync the `arc-runners` ArgoCD app. Until the pool exists, every
   runner pod sits Pending and the merge queue stalls — the values and the
   pool are one rollout, never two.
4. `CI_SELF_HOSTED=off` (repo variable on SocialGouv/iterion) remains the
   emergency lever that routes everything back to ubuntu-latest.

Rollout verification: pods of a queue build land on `ci` nodes
(`kubectl --context ovh-dev -n arc-runners get pod -o wide`), requests
rendered at 4/8Gi, and the `oblik` right-sizing webhook does NOT rewrite
runner pods (its MutatingWebhookConfiguration matches only
deployments/statefulsets/cronjobs — verify post-deploy with
`kubectl --context ovh-dev -n arc-runners get mutatingwebhookconfiguration -o yaml | grep -A5 resources`).

### Docker Hub mirror

`docker run` and `services:` in a job pull through the `dind` daemon, not
through kubelet, so kube-image-keeper never sees those pulls; they leave the
cluster anonymously, and Docker Hub rate-limits anonymous pulls per IP
(`ratelimit-limit: 100;w=3600` on 2026-09-29). iterion's jobs alone start
up to ~30 such pulls an hour at their peak once `mongo-conformance` runs here
(an estimate from that day's job counts), on an egress other workloads
share. `dockerd --registry-mirror=https://mirror.gcr.io` asks Google's mirror
of Docker Hub first (it serves `mongo:8.0` at Docker Hub's digest) and falls
back to Docker Hub on its own when the mirror does not have an image.
