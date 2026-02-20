package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/inodinwetrust10/rewardPoints/internal/db"
	"github.com/inodinwetrust10/rewardPoints/internal/models"
)

type Handler struct {
	s *db.Store
}

func New(s *db.Store) *Handler {
	return &Handler{s: s}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/topup", h.TopUp)
		r.Post("/bonus", h.Bonus)
		r.Post("/spend", h.Spend)
		r.Get("/wallets/{walletId}/balance", h.GetBalance)
		r.Get("/wallets/{walletId}/ledger", h.GetLedger)
	})
}

func (h *Handler) TopUp(w http.ResponseWriter, r *http.Request) {
	h.transfer(w, r, "TOPUP", false)
}

func (h *Handler) Bonus(w http.ResponseWriter, r *http.Request) {
	h.transfer(w, r, "BONUS", false)
}

func (h *Handler) Spend(w http.ResponseWriter, r *http.Request) {
	h.transfer(w, r, "SPEND", true)
}

func (h *Handler) GetBalance(w http.ResponseWriter, r *http.Request) {
	wid, err := strconv.Atoi(chi.URLParam(r, "walletId"))
	if err != nil {
		js(w, http.StatusBadRequest, models.ErrResp{Error: "invalid wallet ID"})
		return
	}
	bal, err := h.s.GetWalletBalance(r.Context(), wid)
	if err != nil {
		if errors.Is(err, db.ErrWalletNotFound) {
			js(w, http.StatusNotFound, models.ErrResp{Error: "wallet not found"})
			return
		}
		log.Printf("GetBalance: %v", err)
		js(w, http.StatusInternalServerError, models.ErrResp{Error: "internal error"})
		return
	}
	js(w, http.StatusOK, bal)
}

func (h *Handler) GetLedger(w http.ResponseWriter, r *http.Request) {
	wid, err := strconv.Atoi(chi.URLParam(r, "walletId"))
	if err != nil {
		js(w, http.StatusBadRequest, models.ErrResp{Error: "invalid wallet ID"})
		return
	}
	entries, err := h.s.GetLedgerEntries(r.Context(), wid)
	if err != nil {
		log.Printf("GetLedger: %v", err)
		js(w, http.StatusInternalServerError, models.ErrResp{Error: "internal error"})
		return
	}
	if entries == nil {
		entries = []models.LedgerEntry{}
	}
	js(w, http.StatusOK, entries)
}

func (h *Handler) transfer(w http.ResponseWriter, r *http.Request, txType string, userIsSrc bool) {
	var req models.TxReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		js(w, http.StatusBadRequest, models.ErrResp{Error: "invalid JSON body"})
		return
	}
	if req.IdempotencyKey == "" {
		js(w, http.StatusBadRequest, models.ErrResp{Error: "idempotency_key is required"})
		return
	}
	if req.UserID == 0 {
		js(w, http.StatusBadRequest, models.ErrResp{Error: "user_id is required"})
		return
	}
	if req.AssetCode == "" {
		js(w, http.StatusBadRequest, models.ErrResp{Error: "asset_code is required"})
		return
	}
	if req.Amount <= 0 {
		js(w, http.StatusBadRequest, models.ErrResp{Error: "amount must be positive"})
		return
	}

	ctx := r.Context()

	uw, err := h.s.GetWalletByOwnerAndAsset(ctx, req.UserID, req.AssetCode)
	if err != nil {
		if errors.Is(err, db.ErrWalletNotFound) {
			js(w, http.StatusNotFound, models.ErrResp{Error: "user wallet not found"})
			return
		}
		log.Printf("resolve user wallet: %v", err)
		js(w, http.StatusInternalServerError, models.ErrResp{Error: "internal error"})
		return
	}

	tw, err := h.s.GetTreasuryWallet(ctx, req.AssetCode)
	if err != nil {
		if errors.Is(err, db.ErrWalletNotFound) {
			js(w, http.StatusNotFound, models.ErrResp{Error: "treasury wallet not found for asset"})
			return
		}
		log.Printf("resolve treasury wallet: %v", err)
		js(w, http.StatusInternalServerError, models.ErrResp{Error: "internal error"})
		return
	}

	from, to := tw.ID, uw.ID
	if userIsSrc {
		from, to = uw.ID, tw.ID
	}

	desc := req.Desc
	if desc == "" {
		desc = txType + " transaction"
	}

	resp, err := h.s.ExecuteTransfer(ctx, from, to, req.Amount, req.IdempotencyKey, desc, txType)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrInsufficientBalance):
			js(w, http.StatusBadRequest, models.ErrResp{Error: "insufficient balance"})
		case errors.Is(err, db.ErrWalletNotFound):
			js(w, http.StatusNotFound, models.ErrResp{Error: "wallet not found"})
		case errors.Is(err, db.ErrInvalidAmount):
			js(w, http.StatusBadRequest, models.ErrResp{Error: "amount must be positive"})
		case errors.Is(err, db.ErrMissingIdempotency):
			js(w, http.StatusBadRequest, models.ErrResp{Error: "idempotency_key is required"})
		default:
			log.Printf("ExecuteTransfer: %v", err)
			js(w, http.StatusInternalServerError, models.ErrResp{Error: "internal error"})
		}
		return
	}

	code := http.StatusCreated
	if resp.Status == "duplicate" {
		code = http.StatusOK
	}
	js(w, code, resp)
}

func js(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("json encode: %v", err)
	}
}
