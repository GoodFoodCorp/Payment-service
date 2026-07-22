# Payment Service

Microservice **Go** propriétaire des **paiements** et seul point d'intégration
avec **Stripe**. Extrait de `order-service`, qui lui délègue désormais la création
des intentions de paiement et leur confirmation.

| | |
|---|---|
| **Langage / techno** | Go 1.26, chi (routeur), pgx (PostgreSQL), zerolog, golang-migrate |
| **Base de données** | PostgreSQL (port hôte `5436`) |
| **Port HTTP** | `8086` |
| **Fournisseur de paiement** | Stripe en **mode test** (ou passerelle factice hors-ligne) |

---

## Architecture — Clean / Hexagonale

```
cmd/main.go                # Démarrage, migrations, injection des dépendances
internal/
├── domain/                # Payment, statuts, erreurs typées,
│                          # ports : PaymentRepository et PaymentGateway
├── application/           # Cas d'usage : créer une intention, confirmer, consulter
├── adapter/
│   ├── http/              # Routeur chi, middleware JWT, DTO
│   ├── postgres/          # Repository pgx + migrations SQL embarquées
│   └── stripe/            # Client Stripe REST + FakeGateway (démo hors-ligne)
└── config/                # Configuration typée depuis l'environnement
```

**Point clé** : le fournisseur de paiement est un **port** (`PaymentGateway`).
Le domaine ignore totalement Stripe — une future intégration EBICS serait un port
frère, sans toucher aux règles métier.

---

## Fonctionnalités

- **Création d'une intention de paiement** pour une commande, auprès de Stripe
  (mode test), avec enregistrement du paiement
- **Idempotence** : rappeler la création pour la même commande réutilise
  l'intention en attente au lieu d'en créer une seconde
- **Refus du double paiement** : une commande déjà payée renvoie `409`
- **Confirmation du paiement** : vérification du statut auprès du fournisseur puis
  passage à `SUCCEEDED` avec horodatage — idempotente elle aussi
- **Consultation du paiement** d'une commande
- **Mode démo hors-ligne** : sans clé Stripe (ou avec une valeur `placeholder`),
  une passerelle factice prend automatiquement le relais et répond « succeeded ».
  Le parcours complet reste démontrable sans réseau ni compte Stripe.
- **Cloisonnement** : seul le client propriétaire du paiement (ou le siège) peut
  le confirmer ou le consulter (`403` sinon)

### Statuts d'un paiement

`PENDING` → `SUCCEEDED` (ou `FAILED`)

---

## Endpoints

| Méthode | Route | Accès |
|---|---|---|
| POST | `/api/payments/intents` | authentifié (appelé par `order-service`) |
| POST | `/api/payments/{orderId}/confirm` | propriétaire, `admin` |
| GET | `/api/payments/{orderId}` | propriétaire, `admin` |
| GET | `/healthz`, `/readyz` | public (sondes) |

**Appel depuis `order-service`** : le JWT du client est transmis tel quel, pour
que le paiement reste attribuable à la personne qui paie (pas de compte de service).

---

## Lancement

```bash
docker network create microservices-net   # une seule fois, partagé
cp .env.example .env                      # renseigner POSTGRES_PASSWORD et JWT_SECRET
docker compose up -d --build
```

⚠️ `JWT_SECRET` doit être **identique** à celui de `auth-service`.

### Variables d'environnement

| Variable | Requis | Description |
|---|---|---|
| `PORT` | non (8086) | Port HTTP |
| `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB` | oui | Base dédiée `payment-db` |
| `DATABASE_URL` | oui | Chaîne pgx (le compose la construit pour le conteneur) |
| `JWT_SECRET` | oui | Secret HS256 partagé avec `auth-service` |
| `STRIPE_SECRET_KEY` | non | Clé test `sk_test_…` ; **vide → passerelle factice** |

---

## Tests

```bash
go test ./internal/... -cover     # couverture ~86 % sur la couche application
go vet ./... && gofmt -l .
```

Couvre l'idempotence, le refus du double paiement, la confirmation et les règles
de propriété. Aucune base ni appel réseau requis.

> ⚠️ **Aucune CI n'est configurée sur ce projet** — les tests doivent être lancés
> manuellement.

---

## Note de production

En production, la confirmation devrait être pilotée par un **webhook Stripe** reçu
par ce service, et non par un appel du client après le paiement. L'endpoint de
confirmation est conçu pour accueillir ce basculement sans changer le domaine.
