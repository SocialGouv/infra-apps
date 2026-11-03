# Valkey 0.10.3 — bootstrap patch for iterion-valkeyha

Source: the committed `iterion/charts/valkey-0.10.3.tgz` from
[CloudPirates](https://github.com/CloudPirates-io/helm-charts/tree/main/charts/valkey),
Apache-2.0. Chart version 0.10.3, application 9.0.0 and the pinned image digest
are unchanged. The local chart is named **valkeyha**: Helm's dependency resolver
keys repositories by chart name, so a local `valkey` alongside the remote
`valkey` 0.25.11 for valkeyfiler fails looking for a `file://` index. The name
keeps the existing effective release name, labels and services.

The patch is `scripts/discover-master.sh`, its ConfigMap/env wiring, the two
bootstrap call sites and the `sentinel.masterDiscovery` settings. All other
upstream sources are retained. `helm dependency update iterion` packages this
directory; commit its `charts/valkeyha-0.10.3.tgz` with the source. The rendered
test repackages the source into a temporary directory and compares every
archive entry, including additions and removals; timestamps are ignored. It
also checks the helper's rollout checksum.

Both the Valkey init container and Sentinel bootstrap now read the complete
host/port pair. A distant candidate must answer `ROLE master`; an unreachable
or replica candidate causes the scan to continue through the other Sentinels.
This follows the [Sentinel client protocol](https://valkey.io/topics/sentinel-clients/).
The init container can recover its own current identity before its server
starts, but that exception is considered only after the distant candidates.
The Sentinel reader verifies its own server's role too.

An observed reply, including a malformed/error reply, is remembered in
`/tmp/valkey/sentinel-seen`. This shared emptyDir survives container retries
within the same pod. A later silence cannot turn that observation into a fresh
cluster and activate the ordinal-zero fallback. No valid master after the
configured attempts is a named failure; no unverified distant address is written. The init
configuration is truncated before a retry writes its new answer.

The existing first-install fallback remains when **no Sentinel has ever
answered in this pod**. This does not solve recovery after all Sentinels and
their observable state disappear, a network partition that makes all of them
unreachable, or split-brain consensus. The observation file is not durable
across pod replacement. Operator recovery of those cases remains separate.
This patch does not change the upstream preStop hook or its limitations.

Settings under `valkeyha.sentinel.masterDiscovery`:

| Setting | Default | Meaning |
|---|---:|---|
| `maxAttempts` | 60 | Complete scans before refusal; 0 retries without this bound |
| `retrySeconds` | 1 | Positive delay between failed scans |
| `commandTimeoutSeconds` | 2 | Whole CLI command, including reply; 0 disables this bound |

The image's BusyBox `timeout -s KILL` bounds the whole command; the CLI's `-t`
only bounds connection setup. Raw RESP errors stay distinguishable from a
transport failure; they cannot be mistaken for an empty first installation.
Credentials for discovery use `VALKEYCLI_AUTH`, not an additional argv value.
The init reader has not started its server; Sentinel's existing local-server
startup wait remains outside these selection-attempt settings.

Local verification, from the infra-apps repository root (Helm, yq and Go):

```sh
helm package iterion/vendor-charts/valkeyha --destination iterion/charts
go test -count=1 -v iterion/tests/valkeyha_test.go
```

The tests execute both complete rendered scripts under POSIX `sh`, with
sequenced CLI peers: stale-first, self-first, promotion after an intervening
silence, remembered state on restart, replica/error/malformed responses, first
installation and self recovery. They inspect the resulting configuration and
selected port. The chart test covers prod/dev/disabled renders, the PDB's exact
pod selector under name/release overrides, complete package freshness and
valkeyfiler independence. The `Iterion Valkey HA` workflow runs this suite on
PRs and pushes touching `iterion/**`, without rebuilding the committed archive
before checking it. No
cluster or paid model is involved.

## Operator rollout

This PR is preparation only. **Jo owns the merge and rollout**:
`applications/iterion.yaml` enables ArgoCD auto-sync against `master`, so merging
the PR is itself a deployment trigger. Do not arm automatic merge.

After Jo chooses that window and merges, the explicit reconciliation command is:

```sh
argocd app get iterion-prod --hard-refresh
argocd app sync iterion-prod
kubectl --context ovh-prod -n iterion rollout status statefulset/iterion-valkeyha --timeout=10m
kubectl --context ovh-prod -n iterion get pdb iterion-valkeyha
```

Before calling the rollout healthy, inspect `SENTINEL master mymaster` on each
Sentinel and `INFO replication` on each Valkey pod: all Sentinels must agree on
the live master, replicas must have their master link up. The PDB permits at
most one unavailable pod for voluntary evictions; it does not govern direct
pod deletion, node failure or StatefulSet rolling updates. See the
[Kubernetes PDB contract](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/).
Rollback is an operator-chosen Git revert of this PR, followed by the same
refresh/sync and health checks. No rollout or rollback is executed by these tests.
