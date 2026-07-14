# ft_moulinette

Correcteur automatique pour la piscine C de 42

## Prérequis

- **Go**
- **Docker Desktop**
- Un compte 42

## Démarrage rapide

### 1. Cloner et configurer les secrets

```bash
git clone https://github.com/tristan-reig/ft_moulinette.git
cd ft-moulinette
cp .env.example .env
```

Remplir le .env avec les valeurs de l'API 42

```bash
MOULINETTE_42_CLIENT_ID=...
MOULINETTE_42_CLIENT_SECRET=...
```

### 2. Récupérer les dépendances Go

```bash
go mod tidy
```

### 3. Builder l'image sandbox

C'est l'image Docker isolée dans laquelle tourne le code des élèves (gcc +
norminette, sans réseau, sans accès au reste de la machine) :

```bash
docker build -f Dockerfile.sandbox -t ft-moulinette-sandbox:latest .
```

Ne touche pas aux fichiers déjà présents (`tests/c00.yaml`, `tests/c01.yaml`,
qui sont déjà complets).

### 4. Lancer le serveur

```bash
go run ./cmd/server
```

```bash
ft_moulinette écoute sur :8080
```

Rendez-vous sur [**http://localhost:8080**](http://localhost:8080), connectez-vous avec votre compte
42, choisissez un sujet, déposez une archive.
