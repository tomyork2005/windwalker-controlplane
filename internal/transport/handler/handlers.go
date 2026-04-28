package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"control-plane/internal/model"
	"control-plane/internal/payment"

	"github.com/go-chi/chi/v5"
)

type ProcessService interface {
	ProcessPaymentCallback(ctx context.Context, req model.CallbackInput) error
}

type Handler struct {
	svc ProcessService
}

func NewHandler(svc ProcessService) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterRoutes(router chi.Router) {
	router.Route("/api/control", func(router chi.Router) {
		router.Post("/payment/cryptocloud/webhook", h.CallbackCryptoCloud)
		router.Post("/payment/platega/webhook", h.CallbackPlatega)
	})
}

func (h *Handler) CallbackCryptoCloud(w http.ResponseWriter, r *http.Request) {
	slog.Info(
		"cryptocloud callback received",
		"method", r.Method,
		"path", r.URL.Path,
		"content_type", r.Header.Get("Content-Type"),
	)

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	req := model.CallbackInput{
		ProviderName: "CryptoCloud",
		Body:         raw,
	}
	err = h.svc.ProcessPaymentCallback(r.Context(), req)
	if err != nil {
		slog.Error("failed to process cryptocloud callback", "err", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) CallbackPlatega(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("platega callback read body", "err", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	slog.Info(
		"platega callback received",
		"method", r.Method,
		"path", r.URL.Path,
		"body", string(raw),
		"merchant_id_present", r.Header.Get("X-MerchantId") != "",
		"secret_present", r.Header.Get("X-Secret") != "",
	)

	req := model.CallbackInput{
		ProviderName: "Platega",
		Headers: map[string]string{
			"X-MerchantId": r.Header.Get("X-MerchantId"),
			"X-Secret":     r.Header.Get("X-Secret"),
		},
		Body: raw,
	}

	if err := h.svc.ProcessPaymentCallback(r.Context(), req); err != nil {
		if errors.Is(err, payment.ErrBadSignature) {
			slog.Warn("platega callback bad signature")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		slog.Error("failed to process platega callback", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
