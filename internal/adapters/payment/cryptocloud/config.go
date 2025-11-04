package cryptocloud

type Config struct {
	ApiKey       string
	ShopID       string
	ApiSecret    string
	BaseURL      string
	OrderTTL     int // hours
	DefaultEmail string
	Methods      []string
}
