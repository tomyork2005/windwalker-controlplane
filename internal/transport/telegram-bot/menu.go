package bot

import (
	"context"
	"control-plane/internal/storage"
	"errors"
	"fmt"
	"strings"

	tele "gopkg.in/telebot.v4"
)

const mainMenuText = `🌍 Wind-Walker VPN — быстрый и надёжный VPN-сервис со стабильным подключением

⚡ Высокая скорость подключения
🎁 Бесплатный пробный доступ на 3 дня
📱 Одна подписка для всех видов устройств
♾ Возможность смотреть YouTube без рекламы`

func (b *Bot) editOrSend(c tele.Context, text string, kb *tele.ReplyMarkup) error {
	if c.Callback() != nil {
		if err := c.Edit(text, kb); err == nil {
			return nil
		}
	}
	return c.Send(text, kb)
}

func (b *Bot) renderMainMenu(c tele.Context) error {
	return b.editOrSend(c, mainMenuText, buildMainMenuKeyboard())
}

func buildMainMenuKeyboard() *tele.ReplyMarkup {
	var kb tele.ReplyMarkup
	kb.Inline(
		kb.Row(kb.Data("🎁 Попробовать бесплатно", string(actTrialPickRegion), "")),
		kb.Row(kb.Data("💳 Купить / Продлить", string(actMenuBuy), "")),
		kb.Row(kb.Data("👤 Моя подписка", string(actMySubscription), "")),
		kb.Row(
			kb.Data("ℹ О нас", string(actAbout), ""),
			kb.Data("💬 Поддержка", string(actSupport), ""),
		),
	)
	return &kb
}

func (b *Bot) renderTrialRegionPicker(ctx context.Context, c tele.Context) error {
	plans, err := b.svc.ListTrialPlans(ctx)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "Не удалось загрузить регионы", ShowAlert: true})
	}
	kb := buildTrialRegionBtns(plans)
	if kb == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Регионы для триала недоступны", ShowAlert: true})
	}
	return b.editOrSend(c, "Выбери регион для пробной подписки:", kb)
}

func (b *Bot) renderMySubscription(ctx context.Context, c tele.Context) error {
	user, err := b.svc.UpsertUserByTelegramID(ctx, c.Sender().ID, c.Sender().Username)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "Не удалось загрузить профиль", ShowAlert: true})
	}

	greeting := "Привет"
	if c.Sender().Username != "" {
		greeting = fmt.Sprintf("Привет, @%s", c.Sender().Username)
	}

	sub, err := b.svc.GetActiveSubscriptionByUser(ctx, user.ID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			text := fmt.Sprintf("%s!\n\nУ тебя пока нет активной подписки.", greeting)
			var kb tele.ReplyMarkup
			kb.Inline(
				kb.Row(kb.Data("🎁 Попробовать бесплатно", string(actTrialPickRegion), "")),
				kb.Row(kb.Data("💳 Купить подписку", string(actMenuBuy), "")),
				kb.Row(kb.Data("⬅ Назад", string(actBackToMain), "")),
			)
			return b.editOrSend(c, text, &kb)
		}
		return c.Respond(&tele.CallbackResponse{Text: "Не удалось загрузить подписку", ShowAlert: true})
	}

	var sbuilder strings.Builder
	fmt.Fprintf(&sbuilder, "%s!\n\n", greeting)
	if sub.IsTrial {
		sbuilder.WriteString("🎁 Пробная подписка\n")
	} else {
		sbuilder.WriteString("✅ Активная подписка\n")
	}
	fmt.Fprintf(&sbuilder, "Тариф: %s\n", sub.PlanName)
	fmt.Fprintf(&sbuilder, "Регион: %s\n", sub.Region)
	fmt.Fprintf(&sbuilder, "Протокол: %s\n", strings.ToUpper(sub.DriverType))
	fmt.Fprintf(&sbuilder, "Действует до: %s\n", sub.EndAt.Format("02 Jan 2006 15:04 UTC"))
	if sub.Creds != nil && *sub.Creds != "" {
		fmt.Fprintf(&sbuilder, "\nДоступ:\n%s", *sub.Creds)
	} else {
		sbuilder.WriteString("\nДоступ ещё готовится — придёт отдельным сообщением.")
	}

	var kb tele.ReplyMarkup
	kb.Inline(
		kb.Row(kb.Data("💳 Продлить", string(actMenuBuy), "")),
		kb.Row(kb.Data("⬅ Назад", string(actBackToMain), "")),
	)
	return b.editOrSend(c, sbuilder.String(), &kb)
}

func (b *Bot) renderAbout(c tele.Context) error {
	text := b.cfg.AboutText
	if text == "" {
		text = "Wind-Walker VPN — быстрый и надёжный VPN-сервис."
	}
	var kb tele.ReplyMarkup
	kb.Inline(kb.Row(kb.Data("⬅ Назад", string(actBackToMain), "")))
	return b.editOrSend(c, text, &kb)
}

func (b *Bot) renderSupport(c tele.Context) error {
	username := strings.TrimPrefix(b.cfg.SupportUsername, "@")
	text := fmt.Sprintf("По любым вопросам пиши @%s", username)
	var kb tele.ReplyMarkup
	kb.Inline(
		kb.Row(kb.URL("Открыть чат поддержки", "https://t.me/"+username)),
		kb.Row(kb.Data("⬅ Назад", string(actBackToMain), "")),
	)
	return b.editOrSend(c, text, &kb)
}
