package bot

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"control-plane/internal/config"
	"control-plane/internal/model"
	"control-plane/internal/service"

	tele "gopkg.in/telebot.v4"
	"gopkg.in/telebot.v4/middleware"
)

type ShopService interface {
	ListPlans(ctx context.Context) ([]*model.Plan, error)
	ListPaymentMethods(ctx context.Context) ([]*model.PaymentMethod, error)
	CreateInvoice(ctx context.Context, planID string, telegramID int64, username string, methodID string, chatID int64) (*model.Invoice, error)
	ActivateTrial(ctx context.Context, telegramID int64, username string, chatID int64) (*model.Subscription, error)
	UpsertUserByTelegramID(ctx context.Context, telegramID int64, username string) (*model.User, error)
	GetActiveSubscriptionByUser(ctx context.Context, userID string) (*model.SubscriptionWithPlan, error)
	HasUsedTrial(ctx context.Context, userID string) (bool, error)
}

type action string

const (
	actMenuBuy        action = "menu_buy"
	actMySubscription action = "my_subscription"
	actAbout          action = "about"
	actSupport        action = "support"
	actBackToMain     action = "back_to_main"
	actTrialActivate  action = "trial_activate"

	actPickRegion        action = "pick_region"
	actPickProtocol      action = "pick_protocol"
	actPickDuration      action = "pick_duration"
	actPickPaymentMethod action = "pick_payment_method"
)

var cbRouter = map[action]func(*Bot, tele.Context, string) error{
	actMenuBuy:           (*Bot).onMenuBuy,
	actMySubscription:    (*Bot).onMySubscription,
	actAbout:             (*Bot).onAbout,
	actSupport:           (*Bot).onSupport,
	actBackToMain:        (*Bot).onBackToMain,
	actTrialActivate:     (*Bot).onTrialActivate,
	actPickRegion:        (*Bot).onPickRegion,
	actPickProtocol:      (*Bot).onPickProtocol,
	actPickDuration:      (*Bot).onPickDuration,
	actPickPaymentMethod: (*Bot).onPickPaymentMethod,
}

type Bot struct {
	cfg        config.TelegramConfig
	svc        ShopService
	bot        *tele.Bot
	state      *SafeState
	webhook    *tele.Webhook
	appCtx     context.Context
	photoCache *PhotoCache
}

func NewBot(ctx context.Context, config config.TelegramConfig, service ShopService) (*Bot, error) {
	hook := &tele.Webhook{
		Listen:         "",
		Endpoint:       &tele.WebhookEndpoint{PublicURL: config.WebhookPublicURL + "/tg/webhook"},
		SecretToken:    config.WebhookSecret,
		MaxConnections: 40,
	}

	tb, err := tele.NewBot(tele.Settings{
		Token:  config.BotToken,
		Poller: hook,
	})
	if err != nil {
		return nil, err
	}

	bot := &Bot{
		cfg:        config,
		svc:        service,
		bot:        tb,
		webhook:    hook,
		state:      NewSafeState(),
		appCtx:     ctx,
		photoCache: NewPhotoCache(config.MainMenuImagePath),
	}
	bot.registerHandlers()
	return bot, nil
}

func (b *Bot) WebhookHandler() http.Handler {
	return b.webhook
}

func (b *Bot) registerHandlers() {
	b.bot.Use(middleware.AutoRespond())

	b.bot.Handle("/start", func(c tele.Context) error {
		ctx, cancel := context.WithTimeout(b.appCtx, 5*time.Second)
		defer cancel()
		return b.renderMainMenu(ctx, c)
	})
	b.bot.Handle("/menu", func(c tele.Context) error {
		ctx, cancel := context.WithTimeout(b.appCtx, 5*time.Second)
		defer cancel()
		return b.renderMainMenu(ctx, c)
	})

	b.bot.Handle(tele.OnCallback, func(c tele.Context) error {
		cb := c.Callback()
		slog.Info("callback received",
			"unique", cb.Unique,
			"data", cb.Data,
			"sender_id", c.Sender().ID,
		)

		a, payload := parseCallback(cb.Data)
		if handler, ok := cbRouter[a]; ok {
			return handler(b, c, payload)
		}

		return c.Respond(&tele.CallbackResponse{
			Text: "Неизвестная кнопка",
		})
	})
}

func (b *Bot) onBackToMain(c tele.Context, _ string) error {
	ctx, cancel := context.WithTimeout(b.appCtx, 5*time.Second)
	defer cancel()
	return b.renderMainMenu(ctx, c)
}

func (b *Bot) onAbout(c tele.Context, _ string) error {
	return b.renderAbout(c)
}

func (b *Bot) onSupport(c tele.Context, _ string) error {
	return b.renderSupport(c)
}

func (b *Bot) onMySubscription(c tele.Context, _ string) error {
	ctx, cancel := context.WithTimeout(b.appCtx, 5*time.Second)
	defer cancel()
	return b.renderMySubscription(ctx, c)
}

func (b *Bot) onTrialActivate(c tele.Context, _ string) error {
	ctx, cancel := context.WithTimeout(b.appCtx, 5*time.Second)
	defer cancel()

	_, err := b.svc.ActivateTrial(ctx, c.Sender().ID, c.Sender().Username, c.Chat().ID)
	switch {
	case errors.Is(err, service.ErrTrialAlreadyUsed):
		return c.Respond(&tele.CallbackResponse{Text: "Триал уже был активирован", ShowAlert: true})
	case errors.Is(err, service.ErrNoTrialPlanAvailable):
		return c.Respond(&tele.CallbackResponse{Text: "Триал сейчас недоступен", ShowAlert: true})
	case err != nil:
		slog.Error("activate trial", "err", err)
		return c.Respond(&tele.CallbackResponse{Text: "Что-то пошло не так", ShowAlert: true})
	}

	return b.renderMySubscription(ctx, c)
}

func (b *Bot) onMenuBuy(c tele.Context, _ string) error {
	ctx, cancel := context.WithTimeout(b.appCtx, 5*time.Second)
	defer cancel()

	plans, err := b.svc.ListPlans(ctx)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "Не удалось загрузить планы", ShowAlert: true})
	}
	b.state.SetActualPlans(c.Sender().ID, plans)

	buttons := buildRegionBtns(plans)
	if buttons == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Пока нет доступных регионов", ShowAlert: true})
	}
	return b.editOrSend(c, "Выберите регион:", buttons)
}

// Buy VPN steps - 1. Pick region -> 2. Pick Protocol -> 3. Pick duration 4. Pick payment method 5. Get payment link

func (b *Bot) onPickRegion(c tele.Context, region string) error {
	plans, ok := b.state.GetActualPlans(c.Sender().ID)
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: "Сессия истекла, открой /menu", ShowAlert: true})
	}

	if region == "__back__" {
		buttons := buildRegionBtns(plans)
		if buttons == nil {
			return b.editOrSend(c, "Нет доступных регионов.", nil)
		}
		return b.editOrSend(c, "Выберите регион:", buttons)
	}

	b.state.Update(c.Sender().ID, func(current orderState, ok bool) (next orderState, keep bool) {
		current.Region = region
		current.Protocol = ""
		return current, true
	})

	buttons := buildProtocolBtns(plans, region)
	if buttons == nil {
		return b.editOrSend(c, "В этом регионе протоколов нет. Выберите другой регион:", buildRegionBtns(plans))
	}
	return b.editOrSend(c, "Выберите протокол:", buttons)
}

func (b *Bot) onPickProtocol(c tele.Context, protocol string) error {
	plans, ok := b.state.GetActualPlans(c.Sender().ID)
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: "Сессия истекла, открой /menu", ShowAlert: true})
	}

	st, ok := b.state.Load(c.Sender().ID)
	if !ok || st.Region == "" {
		return b.editOrSend(c, "Сессия истекла. Выберите регион:", buildRegionBtns(plans))
	}

	if protocol == "__back__" {
		buttons := buildProtocolBtns(plans, st.Region)
		if buttons == nil {
			return b.editOrSend(c, "Нет доступных протоколов", nil)
		}
		return b.editOrSend(c, "Выберите протокол:", buttons)
	}

	_, ok = b.state.Update(c.Sender().ID, func(current orderState, ok bool) (next orderState, keep bool) {
		if !ok || current.Region == "" {
			return orderState{}, false
		}
		current.Protocol = protocol
		return current, true
	})
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: "Сессия закончилась, открой /menu", ShowAlert: true})
	}

	buttons := buildDurationBtns(plans, st.Region, protocol)
	if buttons == nil {
		return b.editOrSend(c, "В этой конфигурации нет тарифов. Выберите другой протокол/регион:", buildProtocolBtns(plans, st.Region))
	}

	return b.editOrSend(c, "Выберите длительность подписки:", buttons)
}

func (b *Bot) onPickDuration(c tele.Context, planID string) error {
	ctx, cancel := context.WithTimeout(b.appCtx, 5*time.Second)
	defer cancel()

	plans, ok := b.state.GetActualPlans(c.Sender().ID)
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: "Сессия истекла, открой /menu", ShowAlert: true})
	}

	methods, err := b.svc.ListPaymentMethods(ctx)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "Не удалось загрузить методы оплаты", ShowAlert: true})
	}

	st, ok := b.state.Load(c.Sender().ID)
	if !ok || st.Region == "" || st.Protocol == "" {
		return c.Respond(&tele.CallbackResponse{Text: "Сессия истекла, открой /menu", ShowAlert: true})
	}

	if planID == "__back__" {
		buttons := buildDurationBtns(plans, st.Region, st.Protocol)
		if buttons == nil {
			return b.editOrSend(c, "Для выбранного региона и протокола тарифов нет.", nil)
		}
		return b.editOrSend(c, "Выберите длительность подписки:", buttons)
	}

	buttons := buildPaymentMethodBtns(methods, planID)
	return b.editOrSend(c, "Выберите способ оплаты:", buttons)
}

func (b *Bot) onPickPaymentMethod(c tele.Context, payload string) error {
	ctx, cancel := context.WithTimeout(b.appCtx, 10*time.Second)
	defer cancel()

	methodID, planID := unpackMethodPayload(payload)
	if planID == "" {
		return c.Respond(&tele.CallbackResponse{Text: "Некорректные данные кнопки", ShowAlert: true})
	}

	inv, err := b.svc.CreateInvoice(ctx, planID, c.Sender().ID, c.Sender().Username, methodID, c.Chat().ID)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "Не удалось создать счёт", ShowAlert: true})
	}

	var kb tele.ReplyMarkup
	kb.Inline(
		kb.Row(kb.URL("Оплатить", inv.CheckoutURL)),
		kb.Row(kb.Data("⬅ Назад", string(actBackToMain), "")),
	)

	b.state.Delete(c.Sender().ID)

	return b.editOrSend(c, "Счёт создан ✅\nПосле оплаты подключение придёт автоматически.", &kb)
}

func (b *Bot) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		b.bot.Start()
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		b.bot.Stop()
		<-errCh
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func parseCallback(data string) (action, string) {
	data = strings.TrimPrefix(data, "\f")

	parts := strings.SplitN(data, "|", 2)
	act := action(parts[0])

	if len(parts) == 2 {
		return act, parts[1]
	}

	return act, ""
}
