package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"control-plane/internal/domain"
	"control-plane/internal/transport/dto"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type shopService interface {
	ListPlans() ([]domain.Plan, error)
	StartPayment(ctx context.Context, planID string, username string, paymentProvider string) (domain.Invoice, error)
	ProcessPaymentCallback(r *http.Request)
}

type Handler struct {
	service shopService
}

func (h *Handler) Routes() chi.Router {
	router := chi.NewRouter()

	router.Use(loggingMiddleware)
	router.Use(middleware.Recoverer)

	router.Route("/api/control", func(router chi.Router) {

		router.Get("/plans", h.ListPlans)
		router.Get("/payments/initiate", h.InitiatePayment)
		router.Get("/payment/webhook", h.ProcessPaymentCallback)
	})

	return router
}

func (h *Handler) InitiatePayment(w http.ResponseWriter, r *http.Request) {
	var req dto.InitiatePaymentRequest

	dec := json.NewDecoder(r.Body)
	err := dec.Decode(&req)
	if err != nil {
		respondJson(w, http.StatusBadRequest, fmt.Sprintf("bad json: %s", err))
		return
	}

	err = req.Validate()
	if err != nil {
		respondJson(w, http.StatusBadRequest, err.Error())
		return
	}

	invoice, err := h.service.StartPayment(r.Context(), req.PlanID, req.Username, req.MethodID)
	if err != nil {
		respondJson(w, http.StatusInternalServerError, err.Error())
	}

	out := dto.FromDomainInvoiceToInitiatePaymentResponse(invoice)
	respondJson(w, http.StatusOK, out)
}

func (h *Handler) ProcessPaymentCallback(w http.ResponseWriter, r *http.Request) {
	h.service.ProcessPaymentCallback(r)
}

func (h *Handler) ListPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := h.service.ListPlans()
	if err != nil {
		respondJson(w, http.StatusInternalServerError, err)
		return
	}

	out := make([]dto.PlanDTO, len(plans))
	for i, plan := range plans {
		out[i] = dto.FromDomainPlanToDTO(plan)
	}

	respondJson(w, http.StatusOK, out)
}

func respondJson(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
