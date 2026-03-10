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
	UserID     string `json:"user_id"`
	DriverType string `json:"driver_type"`

	SubscriptionID string `json:"subscription_id"`
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
