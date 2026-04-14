# Huginn

Plateforme de veille technique automatisée déployée sur Kubernetes via ArgoCD.

## Architecture

- **Web** : serveur Puma (port 3000) — interface UI et API
- **Worker** : processus threaded.rb — scheduling, background jobs, appels LLM
- **PostgreSQL** : cluster CNPG (1 instance dev, 2 instances prod)
- **Secrets** : SealedSecrets (un par cluster, conditionnés via `values.cluster`)

## Déploiement

| Cluster | Domaine | Namespace |
|---------|---------|-----------|
| ovh-dev | huginn.dev.fabrique.social.gouv.fr | huginn |
| ovh-prod | huginn.fabrique.social.gouv.fr | huginn |

L'ApplicationSet `applications/huginn.yaml` déploie sur les deux clusters avec des `valueFiles` par environnement :
- `values.yaml` — configuration commune
- `values.ovh-dev.yaml` / `values.ovh-prod.yaml` — overrides par cluster

## Administration des comptes

### Créer un compte utilisateur

1. Aller sur la page de login de Huginn
2. Cliquer sur **Sign up**
3. Remplir le formulaire avec le code d'invitation (stocké dans le SealedSecret `huginn-secrets`, clé `INVITATION_CODE`)

### Passer un compte en admin

Depuis un poste avec accès kubectl au cluster :

```bash
export KUBECONFIG=/path/to/.kube/config

# Identifier le pod web
POD=$(kubectl get pods -n huginn --context <cluster> \
  -l app.kubernetes.io/component=web \
  -o jsonpath='{.items[0].metadata.name}')

# Promouvoir un utilisateur par email
kubectl exec $POD -n huginn --context <cluster> -- \
  bundle exec rails runner \
  'u = User.find_by(email: "user@example.com"); u.admin = true; u.save!; puts "#{u.email} is now admin"'
```

### Promouvoir le dernier utilisateur inscrit

```bash
kubectl exec $POD -n huginn --context <cluster> -- \
  bundle exec rails runner \
  'u = User.last; u.admin = true; u.save!; puts "#{u.email} is now admin"'
```

### Lister les utilisateurs et leur statut admin

```bash
kubectl exec $POD -n huginn --context <cluster> -- \
  bundle exec rails runner \
  'User.all.each { |u| puts "#{u.email} admin=#{u.admin}" }'
```

### Droits admin

Un compte admin peut :
- Créer et gérer les **User Credentials** (webhooks Mattermost, clés API)
- Voir et modifier tous les agents et scénarios
- Accéder aux logs des agents

Un compte non-admin peut créer et gérer ses propres agents mais ne peut pas créer de credentials.

## Secrets

Les secrets sensibles sont gérés via SealedSecrets, un par cluster :
- `huginn-secrets.sealedsecret.yaml` — ovh-dev
- `huginn-secrets-prod.sealedsecret.yaml` — ovh-prod

### Contenu du secret

| Clé | Description |
|-----|-------------|
| `APP_SECRET_TOKEN` | Secret Rails pour les sessions et cookies |
| `INVITATION_CODE` | Code requis pour créer un compte |
| `OPENAI_API_KEY` | Clé API OpenAI pour les agents LLM |
| `SMTP_USER_NAME` | Identifiant SMTP (tipimail) |
| `SMTP_PASSWORD` | Mot de passe SMTP |

### Re-sceller un secret

```bash
# Extraire les valeurs existantes du cluster source
export KUBECONFIG=/path/to/.kube/config
SMTP_USER=$(kubectl get secret smtp-creds -n vaultwarden --context ovh-prod \
  -o jsonpath='{.data.username}' | base64 -d)
# ... idem pour les autres clés

# Sceller pour le cluster cible
echo '{"apiVersion":"v1","kind":"Secret",...}' \
  | kubeseal --cert https://kubeseal.<cluster>.fabrique.social.gouv.fr/v1/cert.pem \
  --format yaml
```

Les endpoints kubeseal sont séparés par cluster :
- ovh-dev : `kubeseal.ovh.fabrique.social.gouv.fr`
- ovh-prod : `kubeseal.ovh-prod.fabrique.social.gouv.fr`

## Scénarios de veille

Voir [scenarios/README.md](scenarios/README.md) pour la documentation détaillée des pipelines de veille technique.
