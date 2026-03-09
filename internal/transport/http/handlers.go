package http

import (
	"context"
	"control-plane/internal/model"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type ProcessService interface {
	ProcessPaymentCallback(ctx context.Context, req model.CallbackInput) error
}

type Handler struct {
	svc ProcessService
}

func (h *Handler) Routes() chi.Router {
	router := chi.NewRouter()

	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)

	router.Route("/api/control", func(router chi.Router) {
		router.Post("/payment/cryptocloud/webhook", h.CallbackCryptoCloud)
	})

	return router
}

func (h *Handler) CallbackCryptoCloud(w http.ResponseWriter, r *http.Request) {
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
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	return
}
