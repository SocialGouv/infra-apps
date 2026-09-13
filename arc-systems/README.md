# ARC availability and the Iterion merge queue

ARC serves required Iterion CI checks. Its controller can be healthy while
failed runner records keep all capacity occupied. These alerts report that
failure before the merge queue's 60-minute check deadline. They do not require
jobs to be running: a healthy idle scale set has zero runners and one listener.

The existing controller 0.11.0 emits the metrics used here. The chart enables
only its `:8080/metrics` endpoint. Listener metrics stay disabled (`0`), so this
change does not activate 0.11.0's additional `listenerMetrics` requirement.
`PodMonitor` uses the actual rendered `gha-rs-controller` pod label and preserves
ARC's own `namespace` label (the runner namespace, `arc-runners`).

| Alert | Delay | Meaning |
|---|---|---|
| `ARCControllerMetricsUnavailable` | 5 min | Scrape target absent or all controller scrapes failing; availability is unknown. |
| `ARCFailedEphemeralRunner` | 5 min | At least one Failed runner record persists and may hold capacity. |
| `ARCListenerUnavailable` | 5 min | Controller is scraped but the scale set has no Running listener, or its listener series is absent. |
| `ARCRunnersPendingWithoutProgress` | 10 min | Pending runners exist and none is Running; a short cold start does not alert. |

The rules are restricted to the configured scale set. A controller scrape
failure suppresses the derivative listener/progress alert. The failure-record
alert remains independently useful. No receiver is invented here: alerts use
`service=arc-runners` and `severity=critical` and must reach ovh-dev's established
Alertmanager route.

## Before activation

Verified read-only on ovh-dev on 2026-09-13: Prometheus
`prometheus-operator/prom-ovh-dev-prometheus` selects all PodMonitor and rule
namespaces/resources (all four selectors are `{}`), so `monitoring.labels: {}`
is correct here. Alertmanager `prom-ovh-dev-alertmanager` uses the existing
`ovh-dev` Slack receiver by default; these service/severity alerts do not match
its namespace-specific override. The complete Helm render passed server-side
dry-run, including both monitoring CRDs. No resources were applied and no
notification was sent.

These configuration checks do not prove post-deployment collection or delivery.
Complete the remaining activation checks with the infrastructure owner:

1. Locate the owning Prometheus and Alertmanager. Inspect its
   `podMonitorNamespaceSelector`, `podMonitorSelector`, `ruleNamespaceSelector`
   and `ruleSelector`. `arc-systems` must be selected; set `monitoring.labels`
   in `values.yaml` if a release/team label is required. An unselected monitor
   **and rule** cannot alert about their own absence.
2. Verify that the `monitoring.coreos.com` CRDs exist. Render and validate:

   ```sh
   helm dependency build arc-systems
   helm template arc-systems arc-systems --namespace arc-systems \
     | kubectl --context ovh-dev apply --server-side --dry-run=server -f -
   ```

3. Plan the controller rollout with the owner. The existing ArgoCD application
   has no automated sync. Do not delete runners, runner sets or CRDs as part of
   enabling monitoring.
4. After the approved sync, check that `up{job="arc-controller"}` is `1`, that
   the four rules are loaded, and that
   `gha_controller_running_listeners{namespace="arc-runners",name="arc-runners"}`
   is `1`. Also inspect the pending/running/failed series, which retain the same
   scale-set labels. Confirm scrape access if network policies apply.
5. Confirm the existing receiver for `service=arc-runners,severity=critical`.
   With the receiver owner's agreement, use an explicitly labelled synthetic
   alert to verify delivery and resolution. Do not break ARC or shut down its
   listener to test an alert. Record the loaded rule group, live series and
   receiver evidence on SocialGouv/iterion#983 before closing it.

Set `monitoring.enabled=false` to omit the monitoring custom resources on a
cluster without Prometheus Operator. This does **not** make that cluster
monitored. Controller metrics themselves remain enabled.

## Responding

Always name the context explicitly, including for read-only commands:

```sh
kubectl --context ovh-dev -n arc-systems get pods
kubectl --context ovh-dev -n arc-runners get autoscalingrunnersets,ephemeralrunnersets,ephemeralrunners
kubectl --context ovh-dev -n arc-systems logs \
  -l app.kubernetes.io/component=controller-manager --tail=200
kubectl --context ovh-dev -n arc-systems get pods \
  -l app.kubernetes.io/component=runner-scale-set-listener
```

Inspect Failed/Pending objects' status, pod scheduling/image-pull events and
registration logs. Check the pinned runner image in `arc-runners/values.yaml`;
ARC's `--disableupdate` means an obsolete image can fail registration even
while the controller remains healthy. Keep incident evidence before repair;
do not clear Failed records blindly.

To restore Iterion delivery while infrastructure is repaired, an authorised
operator can set the existing repository variable without merging a workflow:

```sh
gh variable set CI_SELF_HOSTED --repo SocialGouv/iterion --body off
```

This changes runner routing for **new workflow runs**; it does not migrate jobs
already queued for ARC. Re-run the affected workflow or re-enqueue the PR as
appropriate, preserving the original failed job evidence. After ARC is verified
healthy, deleting the override restores normal routing:

```sh
gh variable delete CI_SELF_HOSTED --repo SocialGouv/iterion
```

No token or runner registration secret belongs in an alert, log excerpt or PR.

## Local verification

Requires Helm, Python 3 with PyYAML and `promtool` on PATH:

```sh
helm dependency build arc-systems
python3 arc-systems/monitoring/check.py
```

The checker renders the real pinned controller chart, verifies that the monitor
selects its pod and metrics port, checks listener metrics remain disabled,
checks optional selection labels and the disable switch, then uses `promtool`
against the rendered rules. Twelve scenarios cover healthy scale-to-zero,
failure persistence/recovery, missing/zero listener, lost/absent scrape,
pending startup and other scale sets. `PROMTOOL=/path/to/promtool` overrides the
binary. No cluster connection or external notification is made by these tests.

Sources: [GitHub's ARC metrics documentation](https://docs.github.com/en/actions/how-tos/manage-runners/use-actions-runner-controller/deploy-runner-scale-sets#enabling-metrics),
[the installed 0.11.0 metrics definitions](https://github.com/actions/actions-runner-controller/blob/gha-runner-scale-set-0.11.0/controllers/actions.github.com/metrics/metrics.go),
and [the 0.11.0 listener change](https://github.com/actions/actions-runner-controller/releases/tag/gha-runner-scale-set-0.11.0).
