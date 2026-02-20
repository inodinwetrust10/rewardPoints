package db

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/inodinwetrust10/rewardPoints/internal/models"
)

var (
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrInvalidAmount       = errors.New("amount must be positive")
	ErrMissingIdempotency  = errors.New("idempotency_key is required")
)

type Store struct {
	Pool *pgxpool.Pool
}

func NewStore(p *pgxpool.Pool) *Store {
	return &Store{Pool: p}
}

func (s *Store) ExecuteTransfer(
	ctx context.Context,
	fromID, toID int,
	amt int64,
	iKey string,
	desc string,
	txType string,
) (*models.TxResp, error) {

	if amt <= 0 {
		return nil, ErrInvalidAmount
	}
	if iKey == "" {
		return nil, ErrMissingIdempotency
	}

	dup, err := s.findByIKey(ctx, iKey)
	if err != nil {
		return nil, fmt.Errorf("idempotency lookup: %w", err)
	}
	if dup != nil {
		return &models.TxResp{
			TxGroupID:      dup[0].TxGroupID,
			IdempotencyKey: iKey,
			Status:         "duplicate",
			Entries:        dup,
		}, nil
	}

	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			log.Printf("rollback error: %v", err)
		}
	}()

	fst, snd := fromID, toID
	if fst > snd {
		fst, snd = snd, fst
	}

	var fstBal, sndBal int64
	err = tx.QueryRow(ctx, "SELECT balance FROM wallets WHERE id = $1 FOR UPDATE", fst).Scan(&fstBal)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWalletNotFound
		}
		return nil, fmt.Errorf("lock wallet %d: %w", fst, err)
	}

	err = tx.QueryRow(ctx, "SELECT balance FROM wallets WHERE id = $1 FOR UPDATE", snd).Scan(&sndBal)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWalletNotFound
		}
		return nil, fmt.Errorf("lock wallet %d: %w", snd, err)
	}

	srcBal := fstBal
	if fromID == snd {
		srcBal = sndBal
	}
	if srcBal < amt {
		return nil, ErrInsufficientBalance
	}

	gid := uuid.New().String()

	var deb models.LedgerEntry
	err = tx.QueryRow(ctx, `
		INSERT INTO ledger_entries (tx_group_id, idempotency_key, wallet_id, entry_type, amount, tx_type, description)
		VALUES ($1, $2, $3, 'DEBIT', $4, $5, $6)
		RETURNING id, tx_group_id, idempotency_key, wallet_id, entry_type, amount, tx_type, description, created_at`,
		gid, iKey, fromID, amt, txType, desc,
	).Scan(&deb.ID, &deb.TxGroupID, &deb.IdempotencyKey, &deb.WalletID, &deb.EntryType, &deb.Amount, &deb.TxType, &deb.Desc, &deb.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert debit: %w", err)
	}

	var cred models.LedgerEntry
	err = tx.QueryRow(ctx, `
		INSERT INTO ledger_entries (tx_group_id, idempotency_key, wallet_id, entry_type, amount, tx_type, description)
		VALUES ($1, $2, $3, 'CREDIT', $4, $5, $6)
		RETURNING id, tx_group_id, idempotency_key, wallet_id, entry_type, amount, tx_type, description, created_at`,
		gid, iKey, toID, amt, txType, desc,
	).Scan(&cred.ID, &cred.TxGroupID, &cred.IdempotencyKey, &cred.WalletID, &cred.EntryType, &cred.Amount, &cred.TxType, &cred.Desc, &cred.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert credit: %w", err)
	}

	_, err = tx.Exec(ctx, "UPDATE wallets SET balance = balance - $1 WHERE id = $2", amt, fromID)
	if err != nil {
		return nil, fmt.Errorf("update from-wallet: %w", err)
	}

	_, err = tx.Exec(ctx, "UPDATE wallets SET balance = balance + $1 WHERE id = $2", amt, toID)
	if err != nil {
		return nil, fmt.Errorf("update to-wallet: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &models.TxResp{
		TxGroupID:      gid,
		IdempotencyKey: iKey,
		Status:         "created",
		Entries:        []models.LedgerEntry{deb, cred},
	}, nil
}

func (s *Store) GetWalletBalance(ctx context.Context, wid int) (*models.BalResp, error) {
	var r models.BalResp
	err := s.Pool.QueryRow(ctx, `
		SELECT w.id, w.owner_id, a.code, w.balance
		FROM wallets w JOIN asset_types a ON a.id = w.asset_type_id
		WHERE w.id = $1`, wid,
	).Scan(&r.WalletID, &r.OwnerID, &r.AssetCode, &r.Balance)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWalletNotFound
		}
		return nil, err
	}
	return &r, nil
}

func (s *Store) GetLedgerEntries(ctx context.Context, wid int) ([]models.LedgerEntry, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, tx_group_id, idempotency_key, wallet_id, entry_type, amount, tx_type, description, created_at
		FROM ledger_entries WHERE wallet_id = $1 ORDER BY created_at DESC`, wid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []models.LedgerEntry
	for rows.Next() {
		var e models.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TxGroupID, &e.IdempotencyKey, &e.WalletID, &e.EntryType, &e.Amount, &e.TxType, &e.Desc, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *Store) GetWalletByOwnerAndAsset(ctx context.Context, ownerID int, asset string) (*models.Wallet, error) {
	var w models.Wallet
	err := s.Pool.QueryRow(ctx, `
		SELECT w.id, w.owner_id, w.asset_type_id, w.balance, w.created_at
		FROM wallets w JOIN asset_types a ON a.id = w.asset_type_id
		WHERE w.owner_id = $1 AND a.code = $2`, ownerID, asset,
	).Scan(&w.ID, &w.OwnerID, &w.AssetTypeID, &w.Balance, &w.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWalletNotFound
		}
		return nil, err
	}
	return &w, nil
}

func (s *Store) GetTreasuryWallet(ctx context.Context, asset string) (*models.Wallet, error) {
	return s.GetWalletByOwnerAndAsset(ctx, 1, asset)
}

func (s *Store) findByIKey(ctx context.Context, key string) ([]models.LedgerEntry, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, tx_group_id, idempotency_key, wallet_id, entry_type, amount, tx_type, description, created_at
		FROM ledger_entries WHERE idempotency_key = $1 ORDER BY entry_type`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []models.LedgerEntry
	for rows.Next() {
		var e models.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TxGroupID, &e.IdempotencyKey, &e.WalletID, &e.EntryType, &e.Amount, &e.TxType, &e.Desc, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return nil, rows.Err()
	}
	return entries, rows.Err()
}
