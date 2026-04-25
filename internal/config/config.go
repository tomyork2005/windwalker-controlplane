package config

import (
	"github.com/ilyakaznacheev/cleanenv"
	"log"
	"os"
)

type Config struct {
	Postgres       `yaml:"postgres"`
	TelegramConfig `yaml:"telegram"`
	AgentConfig    `yaml:"agent"`
	GRPCConfig     `yaml:"grpc"`
	HTTPConfig     `yaml:"http"`

	// payments config
	CryptoCloudConfig `yaml:"crypto_cloud"`
}

type Postgres struct {
	ConnectionString string `yaml:"connection_string" env-required:"true"`
}

type TelegramConfig struct {
	BotToken         string `yaml:"bot_token" env:"TELEGRAM_BOT_TOKEN"`
	Port             string `yaml:"port" env:"TELEGRAM_PORT"`
	WebhookPublicURL string `yaml:"webhook_public_url" env:"TELEGRAM_PUBLIC_URL"`
	WebhookSecret    string `yaml:"webhook_secret" env:"TELEGRAM_SECRET"`
	SupportUsername  string `yaml:"support_username" env:"TELEGRAM_SUPPORT" env-default:"vpn_support"`
	AboutText        string `yaml:"about_text" env:"TELEGRAM_ABOUT" env-default:"Wind-Walker VPN — быстрый и надёжный VPN-сервис со стабильным подключением."`
}

type AgentConfig struct {
	HeartbeatIntervalMinutes int `yaml:"heartbeat_interval" env:"HEARTBEAT_INTERVAL"`
}

type CryptoCloudConfig struct {
	ApiKey       string   `yaml:"api_key" env:"API_KEY"`
	ShopID       string   `yaml:"shop_id" env:"SHOP_ID"`
	ApiSecret    string   `yaml:"api_secret" env:"API_SECRET"`
	BaseURL      string   `yaml:"base_url" env:"BASE_URL"`
	OrderTTL     int      `yaml:"order_ttl" env:"ORDER_TTL"`
	DefaultEmail string   `yaml:"default_email" env:"DEFAULT_EMAIL"`
	Methods      []string `yaml:"methods" env:"METHODS"`
}

type HTTPConfig struct {
	Port string `yaml:"port" env:"HTTP_PORT"`
}

type GRPCConfig struct {
	Port string `yaml:"port" env:"GRPC_PORT"`
}

func MustLoadConfig() *Config {
	configEnv := os.Getenv("CONFIG_PATH")
	if configEnv == "" {
		log.Fatal("configs path can`t be empty")
	}

	if _, err := os.Stat(configEnv); os.IsNotExist(err) {
		log.Fatalf("configs file doesn't exist %s", err)
	}

	var config Config
	if err := cleanenv.ReadConfig(configEnv, &config); err != nil {
		log.Fatalf("fail with read configs %s", err)
	}

	return &config
}
