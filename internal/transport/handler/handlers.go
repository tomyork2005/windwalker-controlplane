package handler

import (
	"context"
	"control-plane/internal/model"
	"io"
	"log/slog"
	"net/http"

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
	return
}
