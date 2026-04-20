# kube-image-keeper (kuik) v2

Chart umbrella déployant kuik v2 en mode **registry externe** (pas de registry ni de proxy in-cluster).

## Architecture

```
Pod CREATE → MutatingWebhook (kuik manager) → annotation kuik.enix.io/original-images
                                             → réécriture vers Harbor proxy cache si applicable

ClusterReplicatedImageSet → déclare l'équivalence entre docker.io/X et harbor/hub/X
                          → kuik HEAD check Harbor ; si OK, réécrit ; sinon fallback docker.io
```

## Pourquoi pas de registry in-cluster ?

kuik v2 a retiré l'archi v1 (proxy DaemonSet + registry bundlée). Voir
[docs v1→v2 migration](https://github.com/enix/kube-image-keeper/blob/main/docs/v1-to-v2-migration-path.md) :

> v2 removes both components entirely. Image routing is handled at the mutating webhook level.
> Images are pulled directly from external registries without any intermediate layer inside the cluster.

Tentative initiale : docker-registry in-cluster pointé par `ClusterImageSetMirror`. Cassé sur
OVH Managed K8s (containerd nodes ne peuvent pas pull HTTP plain d'un service `.svc.cluster.local`,
et OVH ne permet pas de config `/etc/containerd/certs.d` via DaemonSet hostPath raisonnable).

## Ce que ça couvre

- **Rate limits docker.io** → Harbor proxy cache `/hub` (projet `hub` côté Harbor) sert comme
  source privilégiée (`spec.priority: -1`). Les pulls docker.io massifs lors d'upgrades passent
  par Harbor, qui cache.
- **Indisponibilité docker.io** → fallback automatique sur l'original (`priority 20` implicite)
  via `ClusterReplicatedImageSet`.

## Limite : routage proactif sur pods `Always`

OVH Managed K8s active l'admission plugin `AlwaysPullImages` — tous les pods ont
`imagePullPolicy: Always`, impossible de spécifier autre chose.

kuik v2 [pod_webhook.go:398-399](https://github.com/enix/kube-image-keeper/blob/v2.2.2/internal/webhook/core/v1/pod_webhook.go#L398-L399)
gère les pods `Always` dans une branche spéciale : l'image originale est placée
inconditionnellement en `container.Images[0]`, **avant** les alternatives triées.
`parallel.FirstSuccessful` attend d'abord index 0 et le retourne s'il répond au
HEAD, ce qui **court-circuite le `spec.priority` des CRIS/CISM**.

Conséquence pratique sur OVH :
- **Pas de routage proactif** vers Harbor (`spec.priority: -1` ignoré)
- **Fallback OK** quand docker.io retourne une erreur au HEAD (rate limit 429, timeout, DNS fail)

Bug remonté upstream : voir TODO (issue à ouvrir). En attendant, la protection est
limitée au scénario « upstream en panne au moment du pod CREATE ».

## Ce que ça NE couvre PAS

- **Indisponibilité Harbor** pour les images Fabrique (buildées et poussées uniquement sur Harbor).
  kuik ne peut pas créer de la résilience à partir d'une source unique.
  → Pour y remédier : dual-push CI vers un second registre (ghcr.io, OVH MPR…) puis ajouter
    un `ClusterReplicatedImageSet` supplémentaire.
- **Upgrade du cluster `tools`** (où tourne Harbor). Pendant le rolling, Harbor bouge ; tout
  pod rescheduled qui doit puller une image Fabrique est stuck jusqu'à réveil Harbor.
  → kuik n'est DÉLIBÉRÉMENT PAS déployé sur `tools` : router vers Harbor-sur-tools depuis
    tools-lui-même serait circulaire et amplifierait les indisponibilités d'upgrade.

## Namespaces exclus du webhook

- `kube-system` — composants critiques
- `kyverno` — autre admission controller (ordering)
- `openebs` — stockage
- `cattle-system` — Rancher
- `cert-manager` — dépendance TLS du webhook kuik (deadlock)
- `kuik-system` — kuik lui-même (boucle)

## Prérequis Harbor

Le projet `hub` sur `harbor.fabrique.social.gouv.fr` doit être configuré comme **proxy cache** vers
`docker.io`. Visible via l'API : `GET /api/v2.0/projects/hub` → `registry_id: 4`.

Pour étendre à `ghcr.io`, `quay.io` etc., créer des projets proxy cache supplémentaires côté
Harbor puis ajouter des `ClusterReplicatedImageSet` correspondants.

## Runbook : débrancher kuik en urgence

Symptôme : pods bloqués à cause du webhook ou de Harbor.

```bash
# Effet immédiat : supprimer le webhook — les nouveaux pods passent en images directes
kubectl delete mutatingwebhookconfiguration kube-image-keeper-kuik-mutating-webhook

# Empêcher ArgoCD de recréer
kubectl scale deployment kube-image-keeper-kuik-manager -n kuik-system --replicas=0
# ou désactiver auto-sync dans ArgoCD

# Remettre en route
kubectl scale deployment kube-image-keeper-kuik-manager -n kuik-system --replicas=2
# Force sync ArgoCD pour recréer le webhook
```

Les pods déjà running ne sont **pas impactés** — kuik ne mute qu'au `CREATE`.

## Vérifier l'état

```bash
kubectl get clusterreplicatedimagesets
kubectl logs -n kuik-system -l app.kubernetes.io/name=kuik | grep -i reroute

# Pods réécrits
kubectl get pods -A -o json | jq '.items[] | select(.spec.containers[].image | test("harbor.fabrique.social.gouv.fr/hub")) | {namespace: .metadata.namespace, name: .metadata.name, image: .spec.containers[0].image}'
```
