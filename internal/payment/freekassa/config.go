package freekassa

type Config struct {
	ShopID         int64
	APIKey         string
	Secret1        string
	Secret2        string
	BaseAPI        string
	IPWhitelist    []string
	ConfirmWithYES bool

	DefaultEmail string
}
