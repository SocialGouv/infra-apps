# CNPG operator

## hacks

# 1. Cluster CRD
until this issue is resolved: https://github.com/cloudnative-pg/cloudnative-pg/issues/2574
after sync operator with argocd, run manually:
```sh
kubectl -n cnpg patch crd clusters.postgresql.cnpg.io --type json --patch-file patches/cluster-crd.patch.yaml
```

# 2. Mutating webhook
until this issue is resolved: https://github.com/cloudnative-pg/cloudnative-pg/issues/3753
after sync operator with argocd, run manually:
```sh
kubectl patch mutatingwebhookconfiguration cnpg-mutating-webhook-configuration --type=json  --patch-file patches/webhook.patch.yaml
```