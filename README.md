# ft_moulinette

Correcteur automatique pour la piscine C de 42.

`ft_moulinette` permet de soumettre les exercices de la piscine C, de les compiler et de les tester automatiquement dans une sandbox Docker isolée.

## Prérequis

Avant de commencer, installer :

* **Go**
* **Docker Desktop**
* **Git**
* Un compte 42
* Une application OAuth 42

Docker Desktop doit être démarré pour permettre à la moulinette d'exécuter les corrections dans la sandbox.

## Installation

Cloner le projet :

```bash
git clone https://github.com/tristan-reig/ft-moulinette.git
cd ft-moulinette
```

Télécharger les dépendances Go :

```bash
go mod download
```

## Configuration OAuth 42

Le site utilise l'API 42 pour l'authentification et certaines données comme le classement.

Créer une application sur :

```text
https://profile.intra.42.fr/oauth/applications/new
```

Configurer la Redirect URI avec :

```text
http://localhost:9090/auth/callback
```

Récupérer ensuite le **UID** et le **Secret** de l'application.

## Configuration du `.env`

Créer le fichier `.env` à la racine du projet :

```bash
cp .env.example .env
```

Pour une utilisation locale, il peut être configuré comme ceci :

```env
# Serveur
FT_MOULINETTE_ADDR=:9090

# Tests
FT_MOULINETTE_TESTS_DIR=tests

# Données locales
FT_MOULINETTE_HISTORY_DIR=data/history
FT_MOULINETTE_LOCKS_FILE=data/locked_projects.json
FT_MOULINETTE_POOL_CACHE=data/pool_cache.json
FT_MOULINETTE_POOL_HISTORY=data/pool_history.json

# Administration
FT_MOULINETTE_ADMIN_LOGINS=<login>
FT_MOULINETTE_MAINTENANCE=false

# OAuth 42
FT_42_CLIENT_ID=TON_UID_42
FT_42_CLIENT_SECRET=TON_SECRET_42
FT_42_REDIRECT_URL=http://localhost:9090/auth/callback
FT_42_SCOPE=public

# Campus utilisé pour le classement
FT_MOULINETTE_CAMPUS_NAME=<campus>
```

Les deux variables obligatoires sont :

```env
FT_42_CLIENT_ID=...
FT_42_CLIENT_SECRET=...
```

Les autres disposent de valeurs par défaut, mais les définir explicitement permet de garder une configuration locale claire.

### SSO

Le SSO avec `ft_intra` n'est pas nécessaire pour développer `ft_moulinette` localement.

Les variables suivantes doivent donc rester absentes du `.env` :

```env
# FT_SSO_SECRET=
# FT_SSO_COOKIE_DOMAIN=
# FT_MOULINETTE_LOGIN_URL=
```

Au démarrage, le message suivant est donc normal :

```text
FT_SSO_SECRET absent : SSO inter-services désactivé (dev mono-service)
```

Dans ce mode, `ft_moulinette` utilise directement son propre login OAuth 42.

## Construire la sandbox

Les corrections sont exécutées dans une image Docker dédiée contenant les outils nécessaires à la compilation et aux tests.

Construire l'image une première fois :

```bash
docker build \
  -f Dockerfile.sandbox \
  -t ft-moulinette-sandbox:latest \
  .
```

Vérifier qu'elle existe :

```bash
docker image ls ft-moulinette-sandbox
```

Docker Desktop doit rester lancé pendant l'utilisation de la moulinette.

## Lancer ft_moulinette

Depuis la racine du projet :

```bash
go run ./cmd/server
```

Le serveur doit afficher :

```text
ft_moulinette écoute sur :9090
```

Ouvrir ensuite :

```text
http://localhost:9090
```

Puis :

1. se connecter avec son compte 42 ;
2. sélectionner un sujet ;
3. envoyer une archive `.zip`, `.tar.gz` ou `.rar`, ou utiliser un dépôt Git ;
4. attendre le résultat de la correction.

## Données locales

Les données générées par l'application sont stockées dans :

```text
data/
├── history/
├── locked_projects.json
├── pool_cache.json
└── pool_history.json
```

Le dossier est utilisé notamment pour conserver :

* l'historique des corrections ;
* l'état de verrouillage des sujets ;
* le cache du classement ;
* l'historique du classement.

Il peut être créé manuellement si nécessaire :

```bash
mkdir -p data/history
```

## Session de piscine

Par défaut, la session de piscine utilisée pour le classement est déterminée automatiquement à partir de la date.

Pour forcer une session précise, ajouter les deux variables suivantes :

```env
FT_MOULINETTE_POOL_MONTH=july
FT_MOULINETTE_POOL_YEAR=2026
```

Les deux doivent être définies ensemble.

Pour revenir à la détection automatique, les retirer du `.env`.

## Lancer les tests Go

Pour vérifier le projet :

```bash
go test ./...
```

## Démarrage rapide

Après avoir configuré l'application OAuth 42 et le `.env`, un lancement complet peut se résumer à :

```bash
go mod download

docker build \
  -f Dockerfile.sandbox \
  -t ft-moulinette-sandbox:latest \
  .

go run ./cmd/server
```

Puis ouvrir :

```text
http://localhost:9090
```
