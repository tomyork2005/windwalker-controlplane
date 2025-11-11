package dao

type PlanDAO struct {
	ID           string `db:"id"`
	Name         string `db:"name"`
	Region       string `db:"region"`
	Protocol     string `db:"protocol"`
	Amount       int64  `db:"amount"`
	Currency     string `db:"currency"`
	DurationDays int64  `db:"duration_days"`
	Archived     bool   `db:"archived"`
}

type InvoiceDAO struct {
	ID              string `db:"id"`
	UserID          string `db:"user_id"`
	PlanID          string `db:"plan_id"`
	PaymentProvider string `db:"payment_provider"`
	Amount          int64  `db:"amount"`
	Currency        string `db:"currency"`
	Status          string `db:"status"`
	CheckoutURL     string `db:"checkout_url"`
	CreatedAt       string `db:"created_at"`
	ExpiresAt       string `db:"expires_at"`
	PaidAt          string `db:"paid_at"`
}

type Agent struct {
	ID          string `db:"id"`
	InstanceID  string `db:"instance_id"`
	Region      string `db:"region"`
	Version     string `db:"version"`
	DriverTypes string `db:"driver_type"`
}
