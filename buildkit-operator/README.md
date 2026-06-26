# buildkit-operator

Distributed-BuildKit **control plane**: one hot vanilla `buildkitd` per `(project, arch)`, routed to by
a `/route` API, with **VM-isolated untrusted forks** (Kata/cloud-hypervisor) and off-cluster exposure.
Coexists with the incumbent [buildkit-service](../buildkit-service) on the `prod-build` nodepool.

Deployed by [`applications/buildkit-operator.yaml`](../applications/buildkit-operator.yaml) (ArgoCD
ApplicationSet, cluster `ovh-prod`). The Helm chart is **not vendored** here — it stays canonical in the
operator repo (single source, pinned to the commit that produced the deployed images) and the ovh-prod values are inline in that Application's
`valuesObject`, so there is no separate values file to keep in this directory.

The runtime (Kata) is a separate app: [`applications/kata.yaml`](../applications/kata.yaml).

## Before the first sync — prerequisites (out-of-band today)

These Secrets/namespaces are live on ovh-prod outside this chart. The Application references only their
names; do not commit or inspect cleartext Secret data here.

1. **mTLS + auth Secrets**:
   - `buildkit-operator-daemon-certs` — daemon mTLS cert/key/CA (SAN covers `*.<gateway.host>`).
   - `buildkit-operator-client-certs` — client mTLS material (distributed to CI).
   - `buildkit-operator-auth` — key `token`, the `/route` bearer token.
   - `buildkit-operator-s3` in namespace `buildkit-builds` — S3 cold-cache credentials.

   The S3 Secret is managed as a SealedSecret under `startups/sre/raw/clusters/ovh-prod/buildkit-operator/`.
   The mTLS/auth material is intentionally referenced by name only.

2. **Kyverno exclusion.** The platform's `add-custom-mas-securitycontext` ClusterPolicy (managed
   outside this repo) mutates pods to `allowPrivilegeEscalation: false`, which crashes the **rootless**
   (canonical, trusted) daemons. Exclude the builds runtime namespace (`buildkit-builds`). The Kata node
   plumbing namespace (`buildkit-system`) also needs its own platform exemption for host-path/node setup.
   `PolicyException` is disabled cluster-wide, so the policy's own exclude list is the only lever.

   ```yaml
   # patch where add-custom-mas-securitycontext is defined:
   - op: add
     path: /spec/rules/0/exclude/any/0/resources/namespaces/-
     value: buildkit-builds
   ```

3. **Do not let Helm create `buildkit-builds`.** The namespace is already owned by the platform/Rancher,
   so the Application sets `createNamespaces=false` and references the pre-existing namespace.

## Notes

- **Exposure / DNS** — the gateway is an L4 SNI LoadBalancer; clients reach a daemon by mapping
  `<daemon>.<gateway.host>` to the gateway LB IP via the Action's `gateway-ip` input (the cluster's
  external-dns is ingress-source only, so there is no DNS record for this L4 service). Swap in real
  wildcard DNS later to drop that input.
- **Adoption** — the operator was hand-deployed first; the Argo app intentionally has no `automated`
  sync yet. Review the diff, sync manually, then enable automation once clean.
