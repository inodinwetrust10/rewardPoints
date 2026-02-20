package models

import "time"

type AssetType struct {
	ID   int    `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

type User struct {
	ID        int       `json:"id"`
	Username  string    `json:"username"`
	UserType  string    `json:"user_type"`
	CreatedAt time.Time `json:"created_at"`
}

type Wallet struct {
	ID          int       `json:"id"`
	OwnerID     int       `json:"owner_id"`
	AssetTypeID int       `json:"asset_type_id"`
	Balance     int64     `json:"balance"`
	CreatedAt   time.Time `json:"created_at"`
}

type LedgerEntry struct {
	ID             int64     `json:"id"`
	TxGroupID      string    `json:"tx_group_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	WalletID       int       `json:"wallet_id"`
	EntryType      string    `json:"entry_type"`
	Amount         int64     `json:"amount"`
	TxType         string    `json:"tx_type"`
	Desc           string    `json:"description"`
	CreatedAt      time.Time `json:"created_at"`
}

type TxReq struct {
	IdempotencyKey string `json:"idempotency_key"`
	UserID         int    `json:"user_id"`
	AssetCode      string `json:"asset_code"`
	Amount         int64  `json:"amount"`
	Desc           string `json:"description"`
}

type TxResp struct {
	TxGroupID      string        `json:"tx_group_id"`
	IdempotencyKey string        `json:"idempotency_key"`
	Status         string        `json:"status"`
	Entries        []LedgerEntry `json:"entries"`
}

type BalResp struct {
	WalletID  int    `json:"wallet_id"`
	OwnerID   int    `json:"owner_id"`
	AssetCode string `json:"asset_code"`
	Balance   int64  `json:"balance"`
}

type ErrResp struct {
	Error string `json:"error"`
}
