BEGIN;

CREATE TABLE IF NOT EXISTS asset_types (
    id   SERIAL PRIMARY KEY,
    code VARCHAR(50)  NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL
);

INSERT INTO asset_types (id, code, name) VALUES
    (1, 'GOLD_COINS',     'Gold Coins'),
    (2, 'DIAMONDS',       'Diamonds'),
    (3, 'LOYALTY_POINTS', 'Loyalty Points')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS users (
    id         SERIAL PRIMARY KEY,
    username   VARCHAR(100) NOT NULL UNIQUE,
    user_type  VARCHAR(10)  NOT NULL DEFAULT 'USER'
                CHECK (user_type IN ('USER', 'SYSTEM')),
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

INSERT INTO users (id, username, user_type) VALUES
    (1, 'treasury', 'SYSTEM'),
    (2, 'alice',    'USER'),
    (3, 'bob',      'USER')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS wallets (
    id            SERIAL  PRIMARY KEY,
    owner_id      INT     NOT NULL REFERENCES users(id),
    asset_type_id INT     NOT NULL REFERENCES asset_types(id),
    balance       BIGINT  NOT NULL DEFAULT 0
                  CHECK (balance >= 0),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (owner_id, asset_type_id)
);

INSERT INTO wallets (id, owner_id, asset_type_id, balance) VALUES
    (1, 1, 1, 100000000),
    (2, 1, 2, 100000000),
    (3, 1, 3, 100000000)
ON CONFLICT (id) DO NOTHING;

INSERT INTO wallets (id, owner_id, asset_type_id, balance) VALUES
    (4, 2, 1, 0),
    (5, 2, 2, 0),
    (6, 2, 3, 0),
    (7, 3, 1, 0),
    (8, 3, 2, 0),
    (9, 3, 3, 0)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS ledger_entries (
    id              BIGSERIAL   PRIMARY KEY,
    tx_group_id     UUID        NOT NULL,
    idempotency_key VARCHAR(255) NOT NULL,
    wallet_id       INT         NOT NULL REFERENCES wallets(id),
    entry_type      VARCHAR(10) NOT NULL
                    CHECK (entry_type IN ('CREDIT', 'DEBIT')),
    amount          BIGINT      NOT NULL CHECK (amount > 0),
    tx_type         VARCHAR(50) NOT NULL
                    CHECK (tx_type IN ('TOPUP', 'BONUS', 'SPEND')),
    description     TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ledger_idempotency
    ON ledger_entries (idempotency_key, entry_type);

CREATE INDEX IF NOT EXISTS idx_ledger_wallet
    ON ledger_entries (wallet_id, created_at DESC);

INSERT INTO ledger_entries (tx_group_id, idempotency_key, wallet_id, entry_type, amount, tx_type, description) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'seed-alice-gold', 1, 'DEBIT',  1000, 'BONUS', 'Seed: initial Gold Coins for Alice'),
    ('a0000000-0000-0000-0000-000000000001', 'seed-alice-gold', 4, 'CREDIT', 1000, 'BONUS', 'Seed: initial Gold Coins for Alice');
UPDATE wallets SET balance = balance - 1000 WHERE id = 1;
UPDATE wallets SET balance = balance + 1000 WHERE id = 4;

INSERT INTO ledger_entries (tx_group_id, idempotency_key, wallet_id, entry_type, amount, tx_type, description) VALUES
    ('a0000000-0000-0000-0000-000000000002', 'seed-alice-diamonds', 1, 'DEBIT',  500, 'BONUS', 'Seed: initial Diamonds for Alice'),
    ('a0000000-0000-0000-0000-000000000002', 'seed-alice-diamonds', 5, 'CREDIT', 500, 'BONUS', 'Seed: initial Diamonds for Alice');
UPDATE wallets SET balance = balance - 500 WHERE id = 1;
UPDATE wallets SET balance = balance + 500 WHERE id = 5;

INSERT INTO ledger_entries (tx_group_id, idempotency_key, wallet_id, entry_type, amount, tx_type, description) VALUES
    ('a0000000-0000-0000-0000-000000000003', 'seed-bob-gold', 1, 'DEBIT',  750, 'BONUS', 'Seed: initial Gold Coins for Bob'),
    ('a0000000-0000-0000-0000-000000000003', 'seed-bob-gold', 7, 'CREDIT', 750, 'BONUS', 'Seed: initial Gold Coins for Bob');
UPDATE wallets SET balance = balance - 750 WHERE id = 1;
UPDATE wallets SET balance = balance + 750 WHERE id = 7;

INSERT INTO ledger_entries (tx_group_id, idempotency_key, wallet_id, entry_type, amount, tx_type, description) VALUES
    ('a0000000-0000-0000-0000-000000000004', 'seed-bob-loyalty', 1, 'DEBIT',  300, 'BONUS', 'Seed: initial Loyalty Points for Bob'),
    ('a0000000-0000-0000-0000-000000000004', 'seed-bob-loyalty', 9, 'CREDIT', 300, 'BONUS', 'Seed: initial Loyalty Points for Bob');
UPDATE wallets SET balance = balance - 300 WHERE id = 1;
UPDATE wallets SET balance = balance + 300 WHERE id = 9;

SELECT setval('asset_types_id_seq', (SELECT MAX(id) FROM asset_types));
SELECT setval('users_id_seq',       (SELECT MAX(id) FROM users));
SELECT setval('wallets_id_seq',     (SELECT MAX(id) FROM wallets));

COMMIT;
