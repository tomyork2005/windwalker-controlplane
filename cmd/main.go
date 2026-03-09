package cmd

import (
	"context"
	"control-plane/internal/config"
	"control-plane/internal/payment"
	"control-plane/internal/payment/cryptocloud"
	"control-plane/internal/service"
	"control-plane/internal/storage/pgx"
	tg_bot "control-plane/internal/transport/telegram-bot"
	"log"
	"net/http"
	"time"
)

func main() {
	ctx := context.Background()
	cfg := config.MustLoadConfig()

	storage, err := pgx.New(ctx, cfg.ConnectionString)
	if err != nil {
		log.Fatal(err)
	}

	cryptoProvider := cryptocloud.NewProvider(cfg.CryptoCloudConfig, &http.Client{Timeout: 10 * time.Second})
	payments := payment.NewPayment(cryptoProvider)

	agentService := service.NewService(storage, nil, nil, nil)

	shopService := service.NewService(agentService, payments, storage, nil)

	bot, err := tg_bot.NewBot(ctx)
}
