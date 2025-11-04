package dao

type PlanDAO struct {
	ID       string `db:"id"`
	Name     string `db:"name"`
	Region   string `db:"region"`
	Protocol string `db:"protocol"`
	Amount   int64  `db:"amount"`
	Currency string `db:"currency"`
	Archived bool   `db:"archived"`
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
	ExpiresAt       int64  `db:"expires_at"`
	PaidAt          int64  `db:"paid_at"`
}

type Agent struct {
	ID          string `db:"id"`
	InstanceID  string `db:"instance_id"`
	Region      string `db:"region"`
	Version     string `db:"version"`
	DriverTypes string `db:"driver_type"`
}
