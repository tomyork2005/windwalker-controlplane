package domain

import "time"

type User struct {
	ID        string    `db:"id"`
	TgUserID  string    `db:"tg_user_id"`
	CreatedAt time.Time `db:"created_at"`
}
type Plan struct {
	ID       string `db:"id"`
	Name     string `db:"name"`
	Duration string `db:"duration"`
	Region   string `db:"region"`
	Protocol string `db:"protocol"`
	Price    int64  `db:"price"`
}

type Invoice struct {
	ID          string     `db:"id"`
	UserID      string     `db:"user_id"`
	PlanID      string     `db:"plan_id"`
	Provider    string     `db:"provider"`
	Amount      int64      `db:"amount_cents"`
	Status      string     `db:"status"`
	ProviderRef *string    `db:"provider_ref"` // payment provider id
	CheckoutURL *string    `db:"checkout_url"`
	ExpiresAt   *time.Time `db:"expires_at"`
	CreatedAt   time.Time  `db:"created_at"`
	PaidAt      *time.Time `db:"paid_at"`
}

type Agent struct {
	ID         string `db:"id"`
	Region     string `db:"region"`
	Status     string `db:"status"`
	UsersCount int64  `db:"users_count"`
}

type Subscription struct {
	ID        string    `db:"id"`
	UserID    string    `db:"user_id"`
	PlanID    string    `db:"plan_id"`
	Status    string    `db:"status"`
	StartAt   time.Time `db:"start_at"`
	EndAt     time.Time `db:"end_at"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

type AccessLink struct {
	ID        string    `db:"id"`
	UserID    string    `db:"user_id"`
	AgentID   string    `db:"agent_id"`
	Protocol  string    `db:"protocol"`
	Link      string    `db:"link"`
	ExpiresAt time.Time `db:"expires_at"`
	Revoked   bool      `db:"revoked"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
	RequestID string    `db:"request_id"`
}

type ReferralRelation struct {
	InvitedUserID string    `db:"invited_user_id"`
	InviterUserID string    `db:"inviter_user_id"`
	CreatedAt     time.Time `db:"created_at"`
}
