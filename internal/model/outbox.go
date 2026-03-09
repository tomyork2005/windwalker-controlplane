package model

import "time"

const (
	EventTypeSubscriptionActivated = "subscription_activated"
	EventTypeSubscriptionCancelled = "subscription_cancelled"
)

type SubscriptionActivatedEvent struct {
	UserID            string        `json:"user_id"`
	Region            string        `json:"region"`
	DriverType        string        `json:"driver_type"`
	SubscribeDuration time.Duration `json:"subscribe_time"`

	SubscriptionID string `json:"subscription_id"`
}

type SubscriptionCancelEvent struct {
	SubscriptionID string `json:"subscription_id"`
	UserID         string `json:"user_id"`
	PlanID         string `json:"plan_id"`
	InvoiceID      string `json:"invoice_id"`
}

type OutboxEvent struct {
	ID          string     `db:"id"`
	EventType   string     `db:"event_type"`
	Payload     []byte     `db:"payload"`
	Attempts    int        `db:"attempts"`
	Error       string     `db:"error"`
	CreatedAt   time.Time  `db:"created_at"`
	ProcessedAt *time.Time `db:"processed_at"`
}
