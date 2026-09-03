# ci-secrets — secrets of the `ci-*` namespaces

SealedSecrets applied to the per-startup CI namespaces (`ci-<startup>`). One
directory per cluster, because a SealedSecret is encrypted with that cluster's
own sealing key and cannot be moved across clusters.

Sealing uses the **strict** scope (the kubeseal default): a manifest can only
be unsealed under the `(namespace, name)` pair it was generated for, so there
is one file per namespace and a leaked manifest cannot be replayed elsewhere.

## Why these secrets land everywhere

kontinuous copies **every** secret of a startup's `ci-` namespace into each of
its deployed namespaces, through the `importSecrets` pre-deploy plugin
(`copyAllFromCiNamespace: true`). Anything added here therefore reaches every
review/preprod/prod namespace of that startup on its next deploy.

## `git-auth`

Forge token the in-cluster `degit` checkouts authenticate with.

GitHub throttles anonymous access to the git endpoint `degit` uses to resolve a
ref, **per egress IP**. Once a cluster is over that limit, every kontinuous job
fails at its `degit-repository` / `degit-action` init container with
`could not fetch remote …`, which takes down every deployment of the fleet at
once. Authenticating lifts the limit — identity is enough, no permission is
needed, hence a token with no scope at all.

- Key: `GIT_AUTH_TOKEN`
- Value: a **classic** personal access token of `SocialGroovyBot`, with **no
  scope selected** and no expiration. A scopeless classic token grants exactly
  what an anonymous visitor gets; it only carries an identity for rate-limiting.
- Consumed by kontinuous when `KS_GIT_AUTH_SECRET_ENABLED=true` (see the
  `gitAuth` values of its `job`/`jobs` charts).

### Rotating it

Generate a new token on the bot account, then regenerate every manifest — the
cleartext never touches the repo, it only transits the pipe:

```bash
# the token in a 0600 file, one line, no trailing newline
TOKEN_FILE=/path/to/token

for cluster in ovh-dev ovh-prod; do
  case $cluster in
    ovh-dev)  endpoint=https://kubeseal.ovh.fabrique.social.gouv.fr/v1/cert.pem ;;
    ovh-prod) endpoint=https://kubeseal.ovh-prod.fabrique.social.gouv.fr/v1/cert.pem ;;
  esac
  curl -sS "$endpoint" > /tmp/$cluster.pem
  for f in ci-secrets/$cluster/*.git-auth.sealedsecret.yaml; do
    ns=$(basename "$f" .git-auth.sealedsecret.yaml)
    kubectl create secret generic git-auth --namespace "$ns" \
      --from-file=GIT_AUTH_TOKEN="$TOKEN_FILE" --dry-run=client -o yaml \
      | kubeseal --cert /tmp/$cluster.pem -o yaml > "$f"
  done
done
```

Then check nothing leaked before committing:

```bash
grep -rlE 'gh[pous]_[A-Za-z0-9]{20,}' ci-secrets/ && echo LEAK || echo clean
```

## Adding a namespace

A new `ci-<startup>` namespace needs its own file — the strict scope means an
existing manifest cannot be reused. Generate it with the loop above restricted
to that namespace.

## Why not Vault

The other secrets of these namespaces (`harbor`, `kubeconfig`, …) come from
Vault through the Vault Secrets Operator. That path was unavailable when this
was set up: `vault.fabrique.social.gouv.fr` answers `502` — its nginx is
healthy and its TLS certificate current, but the Vault process behind it is
down. Moving `git-auth` to a `VaultStaticSecret` is the natural follow-up once
Vault is back.
