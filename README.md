# Wallet Service

A high-performance, double-entry ledger wallet service built with **Go** and **PostgreSQL**. Designed for gaming platforms and loyalty rewards systems where data integrity matters.

## Features

- **Double-entry ledger** — every credit/debit creates a balanced pair of entries for full auditability
- **ACID transactions** — all balance mutations happen inside a single PostgreSQL transaction
- **Deadlock avoidance** — wallets are always locked in ascending ID order (`SELECT … FOR UPDATE`)
- **Idempotency** — every request carries an `idempotency_key`; duplicates return the original result
- **Containerized** — one-command startup with Docker Compose

---

## Quick Start

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/) & [Docker Compose](https://docs.docker.com/compose/install/)

### 1. Clone & start

```bash
git clone https://github.com/inodinwetrust10/rewardPoints.git
cd rewardPoints
docker-compose up --build -d
```

This will:
1. Start PostgreSQL 16 and run `seed.sql` automatically.
2. Build and start the Go API server on **port 8080**.

### 2. Verify it's running

```bash
curl http://localhost:8080/health
# → {"status":"ok"}
```

### 3. Stop

```bash
docker-compose down
```

To wipe the database volume and start fresh:

```bash
docker-compose down -v
```

---

## Seed Data

The `seed.sql` script creates the following data:

| Entity | Details |
|--------|---------|
| **Asset Types** | `GOLD_COINS`, `DIAMONDS`, `LOYALTY_POINTS` |
| **System Account** | Treasury (user ID 1) — wallets for every asset with 100M initial supply |
| **User: Alice** (ID 2) | 1,000 Gold Coins, 500 Diamonds |
| **User: Bob** (ID 3) | 750 Gold Coins, 300 Loyalty Points |

All initial balances are created through proper ledger entries from the Treasury.

### Wallet ID Reference

| Wallet ID | Owner | Asset |
|-----------|-------|-------|
| 1 | Treasury | Gold Coins |
| 2 | Treasury | Diamonds |
| 3 | Treasury | Loyalty Points |
| 4 | Alice | Gold Coins |
| 5 | Alice | Diamonds |
| 6 | Alice | Loyalty Points |
| 7 | Bob | Gold Coins |
| 8 | Bob | Diamonds |
| 9 | Bob | Loyalty Points |

---

## API Reference

Base URL: `http://localhost:8080/api/v1`

### POST `/topup` — Wallet Top-up (Purchase)

Credits a user's wallet from the Treasury (simulates a real-money purchase).

```bash
curl -X POST http://localhost:8080/api/v1/topup \
  -H "Content-Type: application/json" \
  -d '{
    "idempotency_key": "topup-alice-001",
    "user_id": 2,
    "asset_code": "GOLD_COINS",
    "amount": 500,
    "description": "Purchased 500 Gold Coins"
  }'
```

### POST `/bonus` — Issue Bonus Credits

Awards free credits from the Treasury (e.g., referral bonus).

```bash
curl -X POST http://localhost:8080/api/v1/bonus \
  -H "Content-Type: application/json" \
  -d '{
    "idempotency_key": "bonus-bob-001",
    "user_id": 3,
    "asset_code": "GOLD_COINS",
    "amount": 100,
    "description": "Referral bonus"
  }'
```

### POST `/spend` — Spend Credits

Deducts credits from a user's wallet back to the Treasury.

```bash
curl -X POST http://localhost:8080/api/v1/spend \
  -H "Content-Type: application/json" \
  -d '{
    "idempotency_key": "spend-alice-001",
    "user_id": 2,
    "asset_code": "GOLD_COINS",
    "amount": 200,
    "description": "Bought power-up item"
  }'
```

### GET `/wallets/{walletId}/balance`

```bash
curl http://localhost:8080/api/v1/wallets/4/balance
# → {"wallet_id":4,"owner_id":2,"asset_code":"GOLD_COINS","balance":1000}
```

### GET `/wallets/{walletId}/ledger`

```bash
curl http://localhost:8080/api/v1/wallets/4/ledger
```

### Error Responses

| Status | Meaning |
|--------|---------|
| `400` | Invalid input, insufficient balance, missing idempotency key |
| `404` | Wallet not found |
| `500` | Internal server error |

---

## Technology Choices

| Component | Choice | Rationale |
|-----------|--------|-----------|
| **Go** | v1.22 | Excellent concurrency support, compiles to a single binary, strong standard library |
| **PostgreSQL** | v16 | Robust ACID compliance, `SELECT … FOR UPDATE` row-level locking, mature ecosystem |
| **chi** | v5 | Lightweight HTTP router, stdlib `net/http` compatible, has useful middleware |
| **pgx** | v5 | Native PostgreSQL driver (no cgo), connection pooling, binary protocol |
| **Docker Compose** | v3.9 | One-command setup, health checks for startup ordering |

---

## Concurrency Strategy

### Problem
Multiple concurrent requests could cause race conditions leading to negative balances, phantom reads, or lost updates.

### Solution: Row-Level Locking with Deterministic Ordering

1. **`SELECT … FOR UPDATE`**: Before reading or modifying any wallet balance, we acquire a row-level exclusive lock within the transaction.

2. **Deterministic lock ordering**: Wallets are always locked in **ascending ID order**, regardless of which is the source/destination. This prevents deadlocks where Transaction A locks Wallet 1 then waits for Wallet 2, while Transaction B locks Wallet 2 then waits for Wallet 1.

3. **CHECK constraint**: The database enforces `balance >= 0` as a final safety net, so even if application logic has a bug, the DB will reject negative balances.

4. **Single transaction**: The lock acquisition, balance validation, ledger entry insertion, and balance update all happen in one PostgreSQL transaction with `READ COMMITTED` isolation.

### Idempotency

Every mutation endpoint requires an `idempotency_key` in the request body. The `ledger_entries` table has a unique index on `(idempotency_key, entry_type)`. If a duplicate key is submitted:
- The existing entries are returned with `"status": "duplicate"`.
- No new entries are created, and no balances are modified.
- The HTTP status is `200 OK` instead of `201 Created`.

This makes it safe to retry requests without risk of double-processing.

---

## Architecture: Double-Entry Ledger

Every operation creates **two** `ledger_entries` with the same `tx_group_id`:

| Flow | DEBIT (from) | CREDIT (to) |
|------|-------------|-------------|
| **Top-up** | Treasury | User |
| **Bonus** | Treasury | User |
| **Spend** | User | Treasury |

The `wallets.balance` column is a **denormalized cache** that is updated atomically within the same transaction. The true source of truth is the sum of ledger entries:

```sql
-- Verify balance integrity
SELECT w.id, w.balance AS cached,
       COALESCE(SUM(CASE WHEN le.entry_type = 'CREDIT' THEN le.amount ELSE 0 END), 0) -
       COALESCE(SUM(CASE WHEN le.entry_type = 'DEBIT'  THEN le.amount ELSE 0 END), 0) AS computed
FROM wallets w
LEFT JOIN ledger_entries le ON le.wallet_id = w.id
GROUP BY w.id, w.balance;
```

---

## Project Structure

```
rewardPoints/
├── cmd/server/main.go         # Application entrypoint
├── internal/
│   ├── models/models.go       # Domain types & API contracts
│   ├── db/db.go               # Database store & transactional logic
│   └── handler/handler.go     # HTTP handlers & routing
├── seed.sql                   # Schema + seed data
├── Dockerfile                 # Multi-stage build
├── docker-compose.yml         # One-command startup
├── go.mod
├── go.sum
└── README.md
```
