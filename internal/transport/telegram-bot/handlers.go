package bot

import (
	"context"
	"fmt"
	"time"

	"control-plane/internal/config"
	"control-plane/internal/model"

	tele "gopkg.in/telebot.v4"
	"gopkg.in/telebot.v4/middleware"
)

type ShopService interface {
	ListPlans(ctx context.Context) ([]*model.Plan, error)
	ListPaymentMethods(ctx context.Context) ([]*model.PaymentMethod, error)
	CreateInvoice(ctx context.Context, planID, username, methodID, chatID string) (*model.Invoice, error)
}

type action string

const (
	actPickRegion        action = "pick_region"
	actPickProtocol      action = "pick_protocol"
	actPickDuration      action = "pick_duration"
	actPickPaymentMethod action = "pick_payment_method"

	actMenuBuy     action = "menu_buy"
	actMenuProfile action = "profile"
)

var cbRouter = map[action]func(*Bot, tele.Context, string) error{
	actPickRegion:        (*Bot).onPickRegion,
	actPickProtocol:      (*Bot).onPickProtocol,
	actPickDuration:      (*Bot).onPickDuration,
	actPickPaymentMethod: (*Bot).onPickPaymentMethod,
	actMenuBuy:           (*Bot).onMenuBuy,
	actMenuProfile:       (*Bot).onMenuProfile,
}

type Bot struct {
	cfg    config.TelegramConfig
	svc    ShopService
	bot    *tele.Bot
	state  *SafeState
	appCtx context.Context
}

func NewBot(ctx context.Context, config config.TelegramConfig, service ShopService) (*Bot, error) {
	hook := &tele.Webhook{
		Listen:         config.Port,
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
		cfg:    config,
		svc:    service,
		bot:    tb,
		state:  NewSafeState(),
		appCtx: ctx,
	}
	bot.registerHandlers()
	return bot, nil
}

func (b *Bot) registerHandlers() {
	b.bot.Use(middleware.Logger())
	b.bot.Use(middleware.AutoRespond())

	var mainMenu tele.ReplyMarkup
	btnProfile := mainMenu.Data("Профиль", string(actMenuProfile), "")
	btnBuy := mainMenu.Data("💳 Купить / Продлить", string(actMenuBuy), "")

	mainMenu.Inline(
		mainMenu.Row(btnProfile),
		mainMenu.Row(btnBuy),
	)

	b.bot.Handle("/start", func(c tele.Context) error {
		return c.Send("Главное меню:", &mainMenu)
	})
	b.bot.Handle("/menu", func(c tele.Context) error {
		return c.Send("Главное меню:", &mainMenu)
	})

	b.bot.Handle(tele.OnCallback, func(c tele.Context) error {
		a := action(c.Callback().Unique)
		data := c.Callback().Data
		if handler, ok := cbRouter[a]; ok {
			return handler(b, c, data)
		}
		return nil
	})
}

func (b *Bot) onMenuProfile(c tele.Context, _ string) error {
	return c.Edit("Твой профиль (заглушка). Скоро тут покажем активные подписки и статистику.")
}

func (b *Bot) onMenuBuy(c tele.Context, _ string) error {
	ctx, cancel := context.WithTimeout(b.appCtx, 5*time.Second)
	defer cancel()

	plans, err := b.svc.ListPlans(ctx)
	if err != nil {
		return c.Edit("Не удалось загрузить планы, попробуйте позже 🙏")
	}
	b.state.SetActualPlans(c.Sender().ID, plans)

	buttons := buildRegionBtns(plans)
	if buttons == nil {
		return c.Edit("Пока нет доступных регионов.")
	}
	return c.Edit("Выберите регион:", buttons)
}

// Buy VPN steps - 1. Pick region -> 2. Pick Protocol -> 3. Pick duration 4. Pick payment method 5. Get payment link

func (b *Bot) onPickRegion(c tele.Context, region string) error {
	plans, ok := b.state.GetActualPlans(c.Sender().ID)
	if !ok {
		return c.Edit("Something wrong")
	}

	if region == "__back__" {
		buttons := buildRegionBtns(plans)
		if buttons == nil {
			return c.Edit("Нет доступных регионов.", nil)
		}
		return c.Edit("Выберите регион:", buttons)
	}

	b.state.Update(c.Sender().ID, func(current orderState, ok bool) (next orderState, keep bool) {
		current.Region = region
		current.Protocol = "" // if we choose region again, protocol should be empty
		return current, true
	})

	buttons := buildProtocolBtns(plans, region)
	if buttons == nil {
		return c.Edit("В этом регионе протоколов нет. Выберите другой регион:", buildRegionBtns(plans))
	}
	return c.Edit("Выберите протокол:", buttons)
}

func (b *Bot) onPickProtocol(c tele.Context, protocol string) error {
	plans, ok := b.state.GetActualPlans(c.Sender().ID)
	if !ok {
		return c.Edit("Something wrong")
	}

	st, ok := b.state.Load(c.Sender().ID)
	if !ok || st.Region == "" {
		return c.Edit("Сессия истекла. Выберите регион:", buildRegionBtns(plans))
	}

	if protocol == "__back__" {
		buttons := buildProtocolBtns(plans, st.Region)
		if buttons == nil {
			return c.Edit("Нет доступных протоколов")
		}
		return c.Edit("Выберите протокол:", buttons)
	}

	_, ok = b.state.Update(c.Sender().ID, func(current orderState, ok bool) (next orderState, keep bool) {
		if !ok || current.Region == "" {
			return orderState{}, false // dont have valid step
		}
		current.Protocol = protocol
		return current, true
	})
	if !ok {
		return c.Edit("Сессия закончилась. Пожалуйста, начните заново: /menu")
	}

	buttons := buildDurationBtns(plans, st.Region, protocol)
	if buttons == nil {
		return c.Edit("В этой конфигурации нет тарифов. Выберите другой протокол/регион:", buildProtocolBtns(plans, st.Region))
	}

	return c.Edit("Выберите длительность подписки:", buttons)
}

func (b *Bot) onPickDuration(c tele.Context, planID string) error {
	ctx, cancel := context.WithTimeout(b.appCtx, 5*time.Second)
	defer cancel()

	methods, err := b.svc.ListPaymentMethods(ctx)
	if err != nil {
		return c.Send("Не удалось загрузить методы, попробуйте позже 🙏")
	}

	st, ok := b.state.Load(c.Sender().ID)
	if !ok || st.Region == "" || st.Protocol == "" {
		return c.Edit("Сессия истекла. Начните заново: /menu")
	}
	buttons := buildPaymentMethodBtns(methods, planID)
	return c.Edit("Выберите способ оплаты:", buttons)
}

func (b *Bot) onPickPaymentMethod(c tele.Context, payload string) error {
	ctx, cancel := context.WithTimeout(b.appCtx, 10*time.Second)
	defer cancel()

	methodID, planID := unpackMethodPayload(payload)
	if planID == "" {
		return c.Respond(&tele.CallbackResponse{Text: "Некорректные данные кнопки"})
	}
	username := c.Sender().Username
	if username == "" {
		username = fmt.Sprintf("tg_%d", c.Sender().ID)
	}

	inv, err := b.svc.CreateInvoice(ctx, planID, username, methodID, fmt.Sprintf("%d", c.Chat().ID))
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "Не удалось создать счёт"})
	}

	var kb tele.ReplyMarkup
	kb.Inline(kb.Row(kb.URL("Оплатить", inv.CheckoutURL)))

	b.state.Delete(c.Sender().ID)

	err = c.Edit("Счёт создан ✅", &kb)
	if err != nil {
		return c.Send("Счёт создан ✅", &kb)
	}
	return nil
}
