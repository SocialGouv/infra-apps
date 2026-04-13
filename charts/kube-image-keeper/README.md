# kube-image-keeper (kuik) v2

Chart umbrella déployant :
- **kuik manager** (2 replicas) — webhook mutant + contrôleur de mirroring
- **docker-registry** (2 replicas) — registry cache locale (Distribution v3)
- **ClusterImageSetMirror** — définit le mirroring de toutes les images vers la registry locale

## Architecture

```
Pod CREATE → MutatingWebhook (kuik manager) → annotation kuik.enix.io/original-images
                                             → routing transparent vers la registry cache

ClusterImageSetMirror → contrôleur kuik → copie les images de tous les pods vers la registry locale
```

En v2, kuik ne réécrit plus l'image dans le spec du pod. Le routing est transparent : l'image affichée reste l'originale, mais le trafic est redirigé vers le cache local quand disponible.

## Namespaces exclus du webhook

- `kube-system` — composants critiques du cluster
- `kyverno` — autre admission controller (risque d'ordering)
- `openebs` — stockage (dépendance de la registry cache)
- `cattle-system` — Rancher
- `cert-manager` — dépendance TLS du webhook kuik (risque de deadlock)

## Runbook : débrancher kuik en urgence

### Symptôme
Les pods ne démarrent pas ou sont bloqués à cause du webhook kuik ou de la registry cache.

### Action immédiate (effet instantané)

```bash
# Supprimer le webhook — tous les nouveaux pods passent en images directes
kubectl delete mutatingwebhookconfiguration kube-image-keeper-kuik-mutating-webhook
```

> Les pods déjà en cours d'exécution ne sont **pas impactés** — kuik ne mute qu'au `CREATE`.

### Empêcher ArgoCD de recréer le webhook

```bash
# Option 1 : scaler le manager à 0
kubectl scale deployment kube-image-keeper-kuik-manager -n kuik-system --replicas=0

# Option 2 : désactiver le auto-sync dans ArgoCD
```

### Revenir à la normale

```bash
# Remettre le nombre de replicas
kubectl scale deployment kube-image-keeper-kuik-manager -n kuik-system --replicas=2

# Forcer un sync ArgoCD pour recréer le webhook
# (ou attendre le prochain sync automatique)
```

### Vérifier l'état

```bash
# Images en cache
kubectl get clusterimagesetmirrors
kubectl logs -n kuik-system -l app.kubernetes.io/name=kuik | grep "mirrored"

# Pods annotés par kuik
kubectl get pods -A -o json | jq '.items[] | select(.metadata.annotations["kuik.enix.io/original-images"]) | {namespace: .metadata.namespace, name: .metadata.name, original: .metadata.annotations["kuik.enix.io/original-images"]}'
```

## Stockage

- PVC 500Gi en `openebs-nfs-hspeed` (Cinder SSD + NFS RWX)
- Garbage collection hebdomadaire (dimanche 3h)
