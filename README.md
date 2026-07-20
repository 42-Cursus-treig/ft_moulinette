# ft_moulinette

Correcteur automatique pour la piscine C de 42

## Prérequis

- **Go**
- **Docker Desktop**
- Un compte 42

## Exécution

Le site est découpé en deux services reliés par un cookie d'identité signé (SSO) :
**ft_moulinette** (correction + classement) et
[**ft_intra**](https://github.com/parragonn/ft_intra) (login/racine + dashboard + agenda).

### En localhost (un seul service, sans SSO)

Chaque repo tourne seul avec son propre login 42. Exemple pour ft_moulinette :

```bash
# 1. Secrets 42 : remplir MOULINETTE_42_CLIENT_ID / _SECRET
cp .env.example .env

# 2. Dépendances + image sandbox (correction isolée : gcc + norminette, sans réseau)
go mod tidy
docker build -f Dockerfile.sandbox -t ft-moulinette-sandbox:latest .

# 3. Lancer
go run ./cmd/server           # écoute sur :8080
```

→ [http://localhost:8080](http://localhost:8080), connexion 42, choix d'un sujet, dépôt d'une archive.
Redirect URI à déclarer sur l'app 42 : `http://localhost:8080/auth/callback`.
`ft_intra` se lance pareil (sans Docker) ; laisser `MOULINETTE_SESSION_SECRET`
vide désactive le SSO — chaque service a alors son propre login.

### Sur serveur (les deux services + SSO + reverse proxy)

```bash
# 1. Secret partagé, IDENTIQUE des deux côtés
openssl rand -hex 32
```

```bash
# 2. ft_moulinette — .env
MOULINETTE_ADDR=:9090
MOULINETTE_42_CLIENT_ID=...
MOULINETTE_42_CLIENT_SECRET=...
MOULINETTE_SESSION_SECRET=<le hex partagé>
MOULINETTE_COOKIE_DOMAIN=.ft-moulinette.fr
MOULINETTE_LOGIN_URL=https://ft-moulinette.fr/login   # login délégué à la racine
```
```bash
docker build -f Dockerfile.sandbox -t ft-moulinette-sandbox:latest .
go build -o ft_moulinette ./cmd/server && ./ft_moulinette      # :9090
```

```bash
# 3. ft_intra — .env (dans son repo)
MOULINETTE_ADDR=:8080
MOULINETTE_42_CLIENT_ID=...
MOULINETTE_42_CLIENT_SECRET=...
MOULINETTE_42_REDIRECT_URL=https://ft-moulinette.fr/auth/callback
MOULINETTE_42_SCOPE=public projects
MOULINETTE_SESSION_SECRET=<le MÊME hex>
MOULINETTE_COOKIE_DOMAIN=.ft-moulinette.fr
```
```bash
go build -o ft_intra ./cmd/server && ./ft_intra                # :8080
```

```caddyfile
# 4. Reverse proxy Caddy (HTTPS auto) — voir ft_intra/deploy/Caddyfile
moulinette.ft-moulinette.fr        { reverse_proxy localhost:9090 }
ft-moulinette.fr, dashboard.ft-moulinette.fr,
agenda.ft-moulinette.fr, projects.ft-moulinette.fr { reverse_proxy localhost:8080 }
# holy-graph.ft-moulinette.fr : appli séparée (SPA), pas servie par ft_intra.
```

5. **DNS** : pointer les sous-domaines vers le serveur.
   **App 42** : redirect URI `https://ft-moulinette.fr/auth/callback`.
   Le login se fait uniquement sur la racine ; les autres sous-domaines
   reconnaissent l'utilisateur via le cookie signé partagé.
