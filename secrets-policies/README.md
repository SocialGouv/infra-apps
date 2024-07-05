# kyverno secrets sync cluster policies

## cluster policies

### common to ci namespace

policy: **sync-secrets-common-to-ci-ns**


```mermaid
graph LR
    sre-secrets -->|eg: buildkit-client-certs| ci-fabrique
    sre-secrets -->|eg: buildkit-client-certs| ci-carnets
```

### internal shared sre secrets

policy: **sync-sre-secrets**


```mermaid
graph LR
    sre-secrets -->|eg: matomo-key| startup-tumeplay-metabase-prod
    sre-secrets -->|eg: matomo-key| startup-ozensemble-metabase-prod
```

### external shared sre secrets

policy: **sync-sre-secrets-to-startups**

```mermaid
graph LR
    sre-secrets -->|eg: pgadmin-oauth2-proxy| fce-preprod
    sre-secrets -->|eg: pgadmin-oauth2-proxy| domifa-preprod
```

label to put on destination namespace:
```yaml
metadata:
  labels:
    secrets.sre.socialgouv.github.io/pgadmin-oauth2-proxy: pgadmin-oauth2-proxy
````

### secrets from startups used by sre

policy: **sync-sre-secrets-from-startups**

label to put on destination namespace:
```yaml
metadata:
  labels:
    sre.socialgouv.github.io/startup: domifa
````


```mermaid
graph LR
    ci-domifa -->|eg: domifa-prod-backups-access-key| startup-domifa-metabase-prod
    ci-domifa -->|eg: domifa-prod-backups-access-key| startup-domifa-metabase-prod
```

### secrets from ci namespace to startup's namespaces


policy: **sync-secrets-from-ci-ns**

based on guaranteed rancher project scope relying on namespace annotation: `field.cattle.io/projectId`

```mermaid
graph LR
    ci-domifa -->|eg: harbor| domifa-feat-awesomeness-and-usefull
    ci-domifa -->|eg: kubeconfig| domifa-feat-awesomeness-and-usefull
```


## re-trigger kyverno (hard refresh)

```sh
./trigger-namespaces
```