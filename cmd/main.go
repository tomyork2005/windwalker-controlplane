package main

import (
	"context"
	controlpb "control-plane/api/control"
	"control-plane/internal/workers"
	"control-plane/internal/workers/cleaner"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"control-plane/internal/config"
	"control-plane/internal/payment"
	"control-plane/internal/payment/cryptocloud"
	"control-plane/internal/payment/platega"
	"control-plane/internal/service"
	"control-plane/internal/storage/pgx"
	"control-plane/internal/transport/agent"
	"control-plane/internal/transport/handler"
	telegram "control-plane/internal/transport/telegram-bot"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.MustLoadConfig()

	storage, err := pgx.New(ctx, cfg.Postgres.ConnectionString)
	log.Printf("dsn=%q", cfg.ConnectionString)
	if err != nil {
		log.Fatal(err)
	}
	err = storage.Ping(ctx)
	if err != nil {
		log.Fatal(err)
	}

	crypto := cryptocloud.NewProvider(cfg.CryptoCloudConfig, &http.Client{Timeout: 10 * time.Second})
	plt := platega.NewProvider(cfg.PlategaConfig, &http.Client{Timeout: 10 * time.Second})
	payments := payment.NewPayments(crypto, plt)

	shop := service.NewShopService(payments, storage, storage)
	process := service.NewProcessService(payments, storage, storage)

	bot, err := telegram.NewBot(ctx, cfg.TelegramConfig, shop)
	if err != nil {
		log.Fatal(err)
	}

	hub := agent.NewHub()
	dispatcher := service.NewDispatcher(storage, hub)

	agentReceiver := service.NewAgentReceiver(storage, time.Minute*time.Duration(cfg.HeartbeatIntervalMinutes))
	agentSender := service.NewAgentSender(storage, dispatcher)

	grpcServer := grpc.NewServer()
	controlpb.RegisterControlPlaneServer(
		grpcServer,
		agent.NewServer(agentReceiver, dispatcher, hub, time.Minute*time.Duration(cfg.HeartbeatIntervalMinutes)),
	)

	worker := workers.NewWorker(storage, agentSender, bot)
	cleaner := cleaner.NewCleaner(storage)

	root := chi.NewRouter()
	root.Use(middleware.Logger)
	root.Use(middleware.Recoverer)

	handler.NewHandler(process).RegisterRoutes(root)

	root.Post("/tg/webhook", func(w http.ResponseWriter, r *http.Request) {
		bot.WebhookHandler().ServeHTTP(w, r)
	})

	httpServer := &http.Server{
		Addr:              cfg.HTTPConfig.Port,
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
	}

	lis, err := net.Listen("tcp", cfg.GRPCConfig.Port)
	if err != nil {
		log.Fatal(err)
	}
	defer lis.Close()

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		log.Printf("http api listening on %s", cfg.HTTPConfig.Port)

		err = httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})

	g.Go(func() error {
		log.Printf("grpc listening on %s", cfg.GRPCConfig.Port)
		return grpcServer.Serve(lis)
	})

	g.Go(func() error {
		log.Printf("worker started")
		worker.Run(ctx)
		return nil
	})

	g.Go(func() error {
		log.Printf("cleaner started")
		cleaner.Run(ctx)
		return nil
	})

	g.Go(func() error {
		log.Printf("telegram bot started")
		err := bot.Run(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	})

	g.Go(func() error {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		grpcServer.GracefulStop()
		return httpServer.Shutdown(shutdownCtx)
	})

	if err = g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("shutdown with error: %v", err)
		os.Exit(1)
	}
}
