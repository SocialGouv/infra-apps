# infra-apps — IaC patterns

GitOps repo deploying SocialGouv apps to OVH-managed Kubernetes (`ovh-dev`, `ovh-prod`) via ArgoCD ApplicationSets + Helm.

## Cluster contexts

- Contexts: `ovh-dev`, `ovh-prod`.
- ⚠️ Always pass `--context ovh-dev|ovh-prod` explicitly when querying, even for read-only commands — never rely on the current context.

## Per-startup app pattern (ApplicationSet `startups-apps`)

Defined in [applications/startups-apps.yaml](applications/startups-apps.yaml). Git-generator over the glob `startups/*/apps/*/envs/*/config.yaml`.

```
startups/<startup>/apps/<app>/
├── Chart.yaml                    # depends on file://./charts/final-appset
├── values.yaml                   # base values
├── templates/
│   ├── appset-env.yaml           # MANDATORY: {{ include "final-appset.include-env" . }}
│   └── *.yaml                    # always-rendered manifests
├── charts/final-appset/          # local subchart, manages namespace + secret cloning
└── envs/<env>/                   # one folder per (env, cluster) target
    ├── config.yaml               # cluster: ovh-dev | ovh-prod
    ├── values.yaml               # env overrides
    └── templates/*.yaml          # env-specific manifests (auto-included via globbing)
```

- ArgoCD app name: `startup-<startup>--<app>-<env>`
- Namespace: same name (auto-created via `final-appset`)
- Auto-injected values: `global.appset.{env,cluster,startup}`
- **Deleting an env's deployment** = remove its `envs/<env>/config.yaml` (the glob no longer matches → ArgoCD prunes the Application + namespace + PVCs).

### `final-appset.include-env` (env templates overlay)

The subchart helper `templates/_appset-env.yaml` provides:

```gotemplate
{{- define "final-appset.include-env" -}}
{{ range $path, $_ := .Files.Glob (printf "envs/%s/templates/**.{yaml,yml}" .Values.global.appset.env) }}
---
{{ tpl ($.Files.Get $path) $ }}
{{ end }}
{{ end }}
```

Without a chart-level `templates/appset-env.yaml` invoking it, files under `envs/<env>/templates/` are **silently ignored**. Add it whenever you need env-specific manifests (NetworkPolicies, sealed secrets, ...).

### `final-appset` namespace knobs (in app values)

```yaml
final-appset:
  importSecretsFromStartup: false
  importSreSecretsList: []        # Kyverno will clone listed secrets from the SRE namespace
  namespace:
    annotations:
      field.cattle.io/projectId: <rancher-project-id>   # Rancher project assignment
    labels:
      cert: wildcard                                     # see "Wildcard cert" below
```

To find a Rancher project ID for a given cluster/startup: `kubectl --context <ctx> get ns <known-ns-of-that-startup> -o jsonpath='{.metadata.annotations.field\.cattle\.io/projectId}'`.

## Kyverno-mirrored projectId label

Kyverno mirrors the annotation `field.cattle.io/projectId: c-m-XXXX:p-YYYY` into a label `rancher.projectId: c-m-XXXX-p-YYYY` (`:` → `-` because labels can't contain `:`). Use it in NetworkPolicies to allow traffic from any namespace in the same Rancher project:

```yaml
- from:
    - namespaceSelector:
        matchLabels:
          rancher.projectId: <mirrored-id>
```

## Wildcard TLS cert (Kyverno-replicated)

- Source: `cert-manager/wildcard-crt`.
- Replication: **Kyverno** copies the secret into any namespace with label `cert: wildcard` (the source carries a `kubed.appscode.com/sync` annotation but kubed is no longer installed — relicat).
- Usage in Ingress: `tls.secretName: wildcard-crt`.

## Sealed Secrets (kubeseal)

- Endpoint per cluster:
  - ovh-dev: `https://kubeseal.ovh.fabrique.social.gouv.fr/v1/cert.pem`
  - ovh-prod: `https://kubeseal.ovh-prod.fabrique.social.gouv.fr/v1/cert.pem`
- Standard command: `kubeseal --cert <endpoint> -f secret.yaml -o yaml --scope cluster-wide > <name>.sealedsecret.yaml`
- `--scope cluster-wide` allows the SealedSecret to be unsealed in any namespace (useful for portability across env-namespaces).
- File naming convention: `*.sealedsecret.yaml`.
- To generate without writing cleartext to disk in the repo:
  ```bash
  kubectl create secret generic <name> --namespace <ns> \
    --from-literal=key="$VALUE" --dry-run=client -o yaml \
    | kubeseal --cert <endpoint> -o yaml --scope cluster-wide > <name>.sealedsecret.yaml
  ```

## Verifying changes

```bash
# Render locally
cd startups/<startup>/apps/<app>
helm dependency update
helm template <release> . -f envs/<env>/values.yaml \
  --set global.appset.env=<env> \
  --set global.appset.cluster=<cluster> \
  --set global.appset.startup=<startup> \
  --namespace startup-<startup>--<app>-<env>

# Server-side dry-run
helm template ... | kubectl --context <cluster> apply --dry-run=server -f -
```

## Shared charts need a hard refresh

Apps pull the shared charts through symlinks (`startups/<startup>/apps/<app>/charts/<chart> -> ../../../../../charts/<chart>`). ArgoCD caches generated manifests per app and does not notice an edit made on the other side of that symlink: a plain sync re-applies the cached render, silently without the change.

⚠️ After editing anything under `charts/`, hard refresh before syncing:

```bash
argocd app get <app> --hard-refresh
argocd app sync <app>
```

`argocd app manifests <app> --revision <sha>` renders fresh and is the quickest way to confirm a shared-chart edit really reaches an app.

Editing a file *inside* the app path invalidates that cache — that is the only reason a change sometimes lands without a hard refresh.

Helm and ArgoCD both read the symlinked source, so in-repo subcharts are never vendored: `helm dependency update` rebuilds `charts/<name>-0.0.0.tgz` locally, and `.gitignore` keeps those out of the repo. Versioned archives (`cnpg-cluster-1.30.6.tgz`, `keda-2.20.1.tgz`, …) are genuine remote dependencies and *are* tracked — don't delete those.
