# Scénario de veille technique

## Architecture du pipeline

Chaque catégorie de veille suit le même pipeline en 5 étapes :

```
Sources RSS  →  Déduplication  →  Digest  →  Synthèse LLM  →  Mattermost
(collecte)     (anti-doublon)    (batch)    (gpt-5.4)        (notification)
```

### Catégories

| Catégorie | Fréquence collecte | Fréquence digest | Sources |
|-----------|--------------------|------------------|---------|
| Cybersécurité | 12h | Quotidien (6h) | CERT-FR, The Hacker News, Krebs, Schneier, Bleeping Computer, Dark Reading |
| IA / LLM | 12h | Quotidien (6h) | Anthropic, OpenAI, Simon Willison, The Gradient, Lilian Weng, HuggingFace, Google AI, HN |
| TypeScript / JavaScript | 24h | Hebdomadaire (lundi) | JS Weekly, Node.js, Deno, Bun, TypeScript, Next.js, HN |
| Go / Python / Rust | 24h | Hebdomadaire (lundi) | Go blog, Python Insider, Rust blog, This Week in Rust, Real Python, weeklies, HN |
| Java / JVM | 24h | Hebdomadaire (lundi) | Inside Java, Baeldung, Spring, JetBrains, HN |

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

## Notification Mattermost

Les synthèses sont envoyées via un `SlackAgent` (API compatible Mattermost) vers un canal dédié. Le webhook URL est stocké dans les User Credentials de Huginn (nom : `mattermost_webhook_url`).

## Import du scénario

### Pré-requis

1. Créer la User Credential `mattermost_webhook_url` dans Huginn (menu Credentials)
2. S'assurer que `OPENAI_API_KEY` est défini dans les variables d'environnement du pod

### Procédure

1. Menu Scenarios → Import Scenario
2. Uploader `veille-tech.json`
3. Les 25 agents sont créés avec leurs connexions

### Schedules de test vs production

Le fichier `veille-tech.json` contient des schedules accélérés pour le test (`every_2h`). Pour la production, modifier via l'UI :

| Agent | Test | Production |
|-------|------|------------|
| RSS (cyber, IA) | every_2h | every_12h |
| RSS (écosystèmes) | every_2h | every_1d |
| Digest quotidien | every_2h | 6am |
| Digest hebdo | every_2h | every_7d |
