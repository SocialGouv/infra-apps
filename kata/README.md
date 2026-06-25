# kata

Kata Containers on the **prod-build** nodepool — the microVM runtime that isolates untrusted
(fork-PR) buildkit daemons spun up by [buildkit-operator](../buildkit-operator). Deployed by
[`applications/kata.yaml`](../applications/kata.yaml) (ArgoCD ApplicationSet, cluster `ovh-prod`).

This directory **is** the upstream `kata-deploy` chart (v3.32.0), vendored. It ships its
`node-feature-discovery` dependency pre-packaged under [`charts/`](charts), so it renders **fully
offline** — Argo's repo-server needs no egress to any Helm repo. The only local addition is one extra
template, [`templates/kata-clh-vcpu-tune.yaml`](templates/kata-clh-vcpu-tune.yaml). NFD/nodeSelector/
tolerations are set by the Application's `valuesObject`, not by editing the vendored chart.

## What lands on the cluster

- **kata-deploy**: installs Kata + the `kata-clh` RuntimeClass on every node of the build pool —
  including replacement nodes MKS recycles in, so it survives a downscale. Reconfigures + restarts
  containerd per node (running pods survive the restart).
- **`kata-clh-vcpu-tune.yaml`** (DaemonSet): raises the guest floor to **4 vCPUs / 4 GiB**. Required
  under nested virt — with the shipped 1 vCPU the kata-agent misses containerd's CRI `get state`
  deadline and the sandbox is killed (VMs loop). Kata reads its config per-sandbox, so no containerd
  restart is needed.

## Why `kata-clh` (cloud-hypervisor), not `kata-qemu`

Under nested virt qemu boots too slowly and the sandbox is killed before it is ready. cloud-hypervisor
boots fast enough. The operator pins sandboxed forks to `runtimeClassName: kata-clh`.

## Updating the vendored chart

Re-copy from the kata-containers repo at the desired tag (the chart already contains its NFD dependency
tgz under `charts/`, so no `helm dependency build` is needed) and keep `templates/kata-clh-vcpu-tune.yaml`:

```sh
git -C kata-src checkout <tag>   # github.com/kata-containers/kata-containers
cp -r kata-src/tools/packaging/kata-deploy/helm-chart/kata-deploy/{Chart.yaml,Chart.lock,values.yaml,charts,templates} kata/
# (then restore kata/templates/kata-clh-vcpu-tune.yaml)
```

## Adoption

Kata is installed by hand on ovh-prod today; the first Argo sync (manual) adopts that release and
re-runs the installer (restarts containerd per node) — do it in a window.

Rationale and the full reproducible setup: the operator repo's `deploy/kata/` and `docs/sandboxed-builds.md`.
