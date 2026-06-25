# buildkit-operator

Distributed-BuildKit **control plane**: one hot vanilla `buildkitd` per `(project, arch)`, routed to by
a `/route` API, with **VM-isolated untrusted forks** (Kata/cloud-hypervisor) and off-cluster exposure.
Coexists with the incumbent [buildkit-service](../buildkit-service) on the `prod-build` nodepool.

Deployed by [`applications/buildkit-operator.yaml`](../applications/buildkit-operator.yaml) (ArgoCD
ApplicationSet, cluster `ovh-prod`). The Helm chart is **not vendored** here — it stays canonical in the
operator repo (single source, pinned to a commit) and the ovh-prod values are inline in that Application's
`valuesObject`, so there is no separate values file to keep in this directory.

The runtime (Kata) is a separate app: [`applications/kata.yaml`](../applications/kata.yaml).

## Before the first sync — prerequisites (out-of-band today)

These three are live on ovh-prod by hand. Fold them into GitOps for a clean cluster rebuild:

1. **mTLS + auth Secrets** in namespace `buildkit-operator` (referenced by `values.ovh.yaml`, not
   created by the chart):
   - `buildkit-operator-daemon-certs` — daemon mTLS cert/key/CA (SAN covers `*.<gateway.host>`).
   - `buildkit-operator-client-certs` — client mTLS material (distributed to CI).
   - `buildkit-operator-auth` — key `token`, the `/route` bearer token.

   They are **mkcert dev** certs today — mint proper certs and seal them (`kubeseal`, see the repo
   CLAUDE.md) before treating this as production-grade. Generator: the operator repo's
   `deploy/cert/create-certs.sh` (supports `GATEWAY_HOST` for the wildcard SAN).

2. **Kyverno exclusion.** The platform's `add-custom-mas-securitycontext` ClusterPolicy (managed
   outside this repo) mutates pods to `allowPrivilegeEscalation: false`, which crashes the **rootless**
   (canonical, trusted) daemons. Add the operator namespace to that policy's `exclude` list — same
   precedent as `arc-runners`/`buildkit-service`. (`PolicyException` is disabled cluster-wide, so the
   policy's own exclude list is the only lever.) Sandboxed *fork* daemons are privileged-in-a-VM and are
   not affected.

   ```yaml
   # patch where add-custom-mas-securitycontext is defined:
   - op: add
     path: /spec/rules/0/exclude/any/0/resources/namespaces/-
     value: buildkit-operator
   ```

3. **Pin the chart `targetRevision`** to a tagged operator release once cut (it is pinned to the
   LB-timeout fix commit for now).

## Notes

- **Exposure / DNS** — the gateway is an L4 SNI LoadBalancer; clients reach a daemon by mapping
  `<daemon>.<gateway.host>` to the gateway LB IP via the Action's `gateway-ip` input (the cluster's
  external-dns is ingress-source only, so there is no DNS record for this L4 service). Swap in real
  wildcard DNS later to drop that input.
- **Adoption** — the operator is hand-deployed today; the first Argo sync adopts that release (review
  the diff, then enable `automated`).
