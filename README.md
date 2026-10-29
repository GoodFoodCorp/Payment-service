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

### Routes publiques (JWT access token)

| Méthode | Route | Accès |
|---|---|---|
| GET | `/api/payments/me?limit=&offset=` | le client (historique paginé, 50 par défaut, 200 max) |
| GET | `/api/payments/{orderId}` | propriétaire, `admin` |
| GET | `/api/payments/methods` | le client (cartes enregistrées) |
| POST | `/api/payments/methods/setup-intent` | le client — renvoie le `client_secret` d'un SetupIntent |
| POST | `/api/payments/methods` | le client — `{payment_method_id, is_default}` après confirmation du SetupIntent |
| DELETE | `/api/payments/methods/{id}` | propriétaire (détachée aussi chez Stripe) |
| POST | `/api/payments/webhooks/stripe` | Stripe — authentifié par l'en-tête `Stripe-Signature` |
| GET | `/healthz`, `/readyz` | public (sondes) |

**Un client ne peut ni créer ni confirmer un paiement** : il n'existe aucune
route publique pour le faire. Le montant est toujours celui calculé par
`order-service` à partir du catalogue.

### Routes internes (`X-Service-Key`, jamais exposées par l'ingress)

| Méthode | Route | Usage |
|---|---|---|
| POST | `/internal/payments/intents` | `order-service` crée/reprend le paiement : `{order_id, customer_id, amount_cents, currency, payment_method_id?}` |
| GET | `/internal/payments/{orderId}` | état du paiement **réconcilié avec Stripe** (confirmation de commande) |
| POST | `/internal/payments/{orderId}/refund` | remboursement (commande payée) ou annulation de l'intention (non payée), idempotent |

### Événements publiés (RabbitMQ `goodfood.events`, via outbox)

| Routing key | Quand |
|---|---|
| `payment.succeeded` | paiement confirmé (webhook ou réconciliation) |
| `payment.failed` | tentative refusée (le client peut réessayer) |
| `payment.refunded` | remboursement effectué (y compris depuis le dashboard Stripe) |

Les événements sont écrits dans la table `outbox_events` **dans la même
transaction** que le changement de statut, puis relayés (au moins une fois) par
un worker qui verrouille les lignes en `FOR UPDATE SKIP LOCKED` : plusieurs
réplicas peuvent tourner sans doublon concurrent.

### Cartes enregistrées (PCI-DSS)

Le numéro de carte ne traverse jamais nos serveurs : le navigateur confirme un
**SetupIntent** avec Stripe Elements, puis envoie uniquement l'identifiant
`pm_…`. Le service vérifie auprès de Stripe que cette carte est bien rattachée
au client Stripe de l'appelant avant de l'enregistrer. En mode factice, les
identifiants de test Stripe (`pm_card_visa`, `pm_card_mastercard`,
`pm_card_amex`) sont acceptés.

### Statuts d'un paiement

`PENDING` → `SUCCEEDED` → `REFUNDED`, ou `PENDING` ⇄ `FAILED` (nouvel essai
possible), ou `PENDING` → `CANCELED` (commande annulée avant paiement).

---

## Dépendances

> **Légende** — 🔴 indispensable (le service ne démarre pas ou ne sert à rien) ·
> 🟠 nécessaire à une fonctionnalité (le reste continue de marcher) ·
> 🟡 optionnelle (dégradation silencieuse, journalisée)

| Dépendance | Type | Conséquence si absente |
|---|---|---|
| **PostgreSQL** (`payment-db`) | 🔴 | Le service ne démarre pas |
| **Stripe** (externe) | 🔴 en production | Hors production, sans clé, une **passerelle factice** approuve tout paiement (démo hors-ligne). Elle est **refusée au démarrage** si `ENV=production` |
| **RabbitMQ** | 🟡 | Les événements restent dans l'outbox et partent au retour du broker |
| **auth-service** | 🟠 | Aucun appel réseau, mais toutes les routes publiques exigent un jeton valide |

**Aucun appel sortant vers un autre service Good Food.**

### Qui dépend de ce service

| Service | Type | Conséquence si `payment-service` est arrêté |
|---|---|---|
| `order-service` | 🟠 | Le paiement et la confirmation de commande échouent (`502`) ; le reste des commandes fonctionne |
| `web-app` | 🟠 | Le bouton « Payer » échoue ; la navigation et l'historique restent utilisables |

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
| `ENV` | non (`development`) | `production` interdit la passerelle factice et exige le secret de webhook |
| `JWT_SECRET` | oui | Secret HS256 des access tokens, partagé avec `auth-service` |
| `JWT_ISSUER` / `JWT_AUDIENCE` | non | Défauts `goodfood-auth` / `goodfood-api` |
| `SERVICE_API_KEY` | oui | Clé `X-Service-Key` des routes `/internal` |
| `PAYMENT_GATEWAY` | non | `stripe` ou `fake` (défaut : `stripe` si une clé est fournie) |
| `STRIPE_SECRET_KEY` | si `stripe` | Clé `sk_test_…` / `sk_live_…` |
| `STRIPE_WEBHOOK_SECRET` | en production | Secret `whsec_…` de l'endpoint webhook |
| `RABBITMQ_URL` | non | Relais des événements de l'outbox |

---

## Tests

```bash
go vet ./... && go test ./...                       # unitaires, sans dépendance
TEST_DATABASE_URL=postgres://...   go test -tags integration ./internal/adapter/postgres/   # vraie base PostgreSQL
```

Couvre : montant imposé par `order-service`, reprise idempotente d'une intention
(avec son `client_secret`), réalignement du montant, remboursement unique,
annulation d'une intention non payée, webhooks (signature, rejeu, événements),
cartes rattachées au bon client Stripe, configuration refusée en production,
rejet des refresh tokens et des routes internes sans clé, outbox transactionnelle.
