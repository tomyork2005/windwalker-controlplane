package domain

const EventTypeSubscriptionActivated = "subscription_activate"

type SubscriptionActivatedEvent struct {
	SubscriptionID string `json:"subscription_id"`
	UserID         string `json:"user_id"`
	PlanID         string `json:"plan_id"`
	InvoiceID      string `json:"invoice_id"`
}
