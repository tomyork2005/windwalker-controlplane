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
