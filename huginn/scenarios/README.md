# Scénarios de veille technique

Deux scénarios sont disponibles : un pour le développement/test, un pour la production.

| Fichier | Usage | Agents | Webhooks | Schedules |
|---------|-------|--------|----------|-----------|
| `veille-tech-dev.json` | Test | 25 | `mattermost_webhook_url_dev` → `#huginn-dev` | `every_2h` |
| `veille-tech-prod.json` | Production | 35 | `mattermost_webhook_url_prod` (multi-canal) + `mattermost_webhook_url_dev` | Réels (6am, every_7d) |

## Architecture du pipeline

Chaque catégorie de veille suit le même pipeline :

```
Sources RSS  →  Déduplication  →  Digest  →  Synthèse LLM  →  Notification(s)
(collecte)     (anti-doublon)    (batch)    (gpt-5.4)        (fan-out)
```

En prod, le LLM envoie sa synthèse vers **plusieurs SlackAgents** en parallèle (fan-out natif Huginn) :

```
                    ┌→ SlackAgent (webhook1, #canal-dédié)
LLM ───────────────┼→ SlackAgent (webhook1, #huginn)
                    └→ SlackAgent (webhook2)
```

### Catégories et routing prod

| Catégorie | Fréquence | webhook1 canaux | webhook2 |
|-----------|-----------|-----------------|----------|
| Cybersécurité | **Quotidien** | `#securite`, `#huginn-dev` | canal par défaut |
| IA / LLM | **Hebdomadaire** | `#g-ia`, `#huginn-dev` | canal par défaut |
| TypeScript / JavaScript | Hebdomadaire | `#veille-tech`, `#huginn-dev` | canal par défaut |
| Go / Python / Rust | Hebdomadaire | `#veille-tech`, `#huginn-dev` | canal par défaut |
| Java / JVM | Hebdomadaire | `#veille-tech`, `#huginn-dev` | canal par défaut |

### Sources RSS

| Catégorie | Sources |
|-----------|---------|
| Cybersécurité | CERT-FR (alertes + avis), The Hacker News, Krebs, Schneier, Bleeping Computer, Dark Reading |
| IA / LLM | Anthropic, OpenAI, Simon Willison, The Gradient, Lilian Weng, HuggingFace, Google AI, HN |
| TypeScript / JavaScript | JS Weekly, Node.js, Deno, Bun, TypeScript, Next.js, HN |
| Go / Python / Rust | Go blog, Python Insider, Rust blog, This Week in Rust, Real Python, weeklies, HN |
| Java / JVM | Inside Java, Baeldung, Spring, JetBrains, HN |

## Protection anti-doublons

Le système garantit qu'une information n'est pas remontée plusieurs fois, via 3 niveaux complémentaires et un filtre sémantique.

### Niveau 1 — RssAgent (par source)

Chaque article RSS possède un identifiant unique (GUID). Le RssAgent stocke en base de données les 1000 derniers IDs vus par agent (`remembered_id_count: 1000`). Un article déjà vu n'est jamais ré-émis, même après un redémarrage du pod. C'est la protection principale.

### Niveau 2 — DeDuplicationAgent (cross-sources)

Si le même article apparait dans deux sources différentes (ex: un CVE repris par The Hacker News et Bleeping Computer), chaque RssAgent l'émet car les GUIDs sont différents. Le DeDuplicationAgent compare les URLs sur les 200 derniers events (`lookback: 200`) et bloque le doublon. Ce state est aussi persisté en base de données.

### Niveau 3 — DigestAgent (fenêtre temporelle)

Le DigestAgent collecte les events reçus depuis le dernier cycle, les agrège en un seul event, puis les oublie (`retained_events: 0`). Au cycle suivant, seuls les nouveaux events sont inclus. Les anciens articles ne sont jamais re-digestés.

### Niveau 4 — LLM (filtre sémantique)

Les 3 niveaux précédents filtrent les doublons techniques (même ID, même URL). Mais deux sources peuvent publier des articles différents sur le même sujet avec des URLs distinctes. Le prompt système du LLM inclut explicitement l'instruction de filtrer les informations redondantes et de fusionner les sujets couverts par plusieurs sources.

### Évolutions d'un sujet

Quand un sujet déjà couvert a du neuf (ex: un patch sort pour un CVE signalé la veille), le nouvel article a un nouvel ID et une nouvelle URL. Il passe les 3 filtres techniques et arrive dans le digest suivant. Le LLM peut le contextualiser par rapport aux informations précédentes.

## Synthèse LLM

Chaque catégorie dispose d'un agent `OpenaiLlmAgent` qui reçoit le digest brut et produit une synthèse structurée.

### Configuration commune

- Modèle : `gpt-5.4`
- Température : `0.3` (factuel, peu créatif)
- Timeout : `300s` (les digests peuvent être volumineux)
- Clé API : variable d'environnement `OPENAI_API_KEY` (fallback automatique du code Huginn)

### Comportement du prompt

Pour chaque digest, le LLM :

1. Filtre les informations redondantes ou mineures
2. Produit un titre explicite et une synthèse de 2-3 phrases par information importante
3. Conserve les liens sources pour approfondir
4. Classe par catégorie avec des emojis de criticité ou de thématique

### Classification par catégorie

| Catégorie | Classification |
|-----------|---------------|
| Cyber | 🔴 Critique, 🟠 Important, 🟡 À noter |
| IA/LLM | 🧠 Modèles & recherche, 🛠️ Outils & frameworks, 💻 Coding assisté par IA |
| TS/JS | 📦 Runtimes, 🔧 Frameworks, 📐 TypeScript, 📚 Écosystème |
| Go/Python/Rust | 🐹 Go, 🐍 Python, 🦀 Rust |
| Java/JVM | ☕ Java core, 🌱 Spring, 🧰 Outils JVM, 📦 Kotlin |

## Import des scénarios

### Pré-requis

1. S'assurer que `OPENAI_API_KEY` est défini dans les variables d'environnement du pod

2. Créer les User Credentials dans Huginn (menu Credentials) :

| Scénario | Credential | Description |
|----------|-----------|-------------|
| Dev | `mattermost_webhook_url_dev` | Webhook verrouillé sur `#huginn-dev` |
| Prod | `mattermost_webhook_url_prod` | Mattermost principal (déverrouillé, multi-canal) |
| Prod | `mattermost_webhook_url_dev` | Réutilisé pour copie dans `#huginn-dev` |

### Procédure

1. Menu Scenarios → Import Scenario
2. Uploader le fichier JSON voulu
3. Les agents sont créés avec leurs connexions

### Ajouter un webhook

Pour ajouter un 3e webhook en prod :

1. Créer la credential (ex: `tchap_webhook`) dans l'UI
2. Dupliquer un des SlackAgents existants par pipeline
3. Changer le `webhook_url` vers `{% credential tchap_webhook %}`
4. Connecter le nouvel agent au LLM correspondant (Sources → ajouter le LLM)
