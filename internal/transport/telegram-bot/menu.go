package bot

import (
	"context"
	"control-plane/internal/storage"
	"errors"
	"fmt"
	"html"
	"strings"

	tele "gopkg.in/telebot.v4"
)

const mainMenuText = `🕊️ <b>Wind-Walker VPN</b> - твои паруса в интернете

— Серверы 10 Гбит/с - стриминг, загрузки и видеозвонки идут так, будто VPN выключен
— Полная приватность - мы не ведём логи и не торгуем данными: ваш трафик остаётся вашим.
— Одна подписка — до 5 устройства

— 3 дня бесплатно, чтобы убедиться`

func (b *Bot) renderMainMenu(ctx context.Context, c tele.Context) error {
	showTrial := true

	user, err := b.svc.UpsertUserByTelegramID(ctx, c.Sender().ID, c.Sender().Username)
	if err == nil {
		used, _ := b.svc.HasUsedTrial(ctx, user.ID)
		showTrial = !used
	}

	return b.sendOrEditIconKeyboard(c, mainMenuText, b.photoCache.Build(mainMenuText), buildMainMenuKeyboard(showTrial))
}

func buildMainMenuKeyboard(showTrial bool) map[string]any {
	rows := make([][]map[string]any, 0, 4)

	if showTrial {
		rows = append(rows, []map[string]any{
			iconCallbackBtn("Попробовать бесплатно", string(actTrialActivate), "", iconTrial),
		})
	}

	rows = append(rows,
		[]map[string]any{iconCallbackBtn("Купить", string(actMenuBuy), "", iconBuy)},
		[]map[string]any{iconCallbackBtn("Моя подписка", string(actMySubscription), "", iconUser)},
		[]map[string]any{
			iconCallbackBtn("О нас", string(actAbout), "", iconAbout),
			iconCallbackBtn("Поддержка", string(actSupport), "", iconSupport),
		},
	)

	return iconKeyboard(rows...)
}

func (b *Bot) renderMySubscription(ctx context.Context, c tele.Context) error {
	greeting := senderGreeting(c)

	user, err := b.svc.UpsertUserByTelegramID(ctx, c.Sender().ID, c.Sender().Username)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: "Не удалось загрузить профиль", ShowAlert: true})
	}

	sub, err := b.svc.GetActiveSubscriptionByUser(ctx, user.ID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			text := fmt.Sprintf("Привет, %s! 👋\n\nУ тебя пока нет активной подписки.", html.EscapeString(greeting))
			kb := iconKeyboard(
				[]map[string]any{iconCallbackBtn("Попробовать бесплатно", string(actTrialActivate), "", iconTrial)},
				[]map[string]any{iconCallbackBtn("Купить подписку", string(actMenuBuy), "", iconBuy)},
				[]map[string]any{iconCallbackBtn("В меню", string(actBackToMain), "", iconHome)},
			)
			return b.sendOrEditIconKeyboard(c, text, nil, kb)
		}
		return c.Respond(&tele.CallbackResponse{Text: "Не удалось загрузить подписку", ShowAlert: true})
	}

	planLabel := sub.PlanName
	if sub.IsTrial {
		planLabel = "Пробный период"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Привет, %s! 👋\n\n", html.EscapeString(greeting))
	sb.WriteString("📊 <b>Моя подписка</b>\n\n")
	fmt.Fprintf(&sb, "🏷 <b>Тариф:</b> %s\n", html.EscapeString(planLabel))
	fmt.Fprintf(&sb, "🌍 <b>Регион:</b> %s\n", html.EscapeString(regionLabel(sub.Region)))
	fmt.Fprintf(&sb, "🛡 <b>Протокол:</b> %s\n", html.EscapeString(strings.ToUpper(sub.DriverType)))
	fmt.Fprintf(&sb, "⏳ <b>Действует до:</b> %s\n", sub.EndAt.Format("02.01.2006 15:04"))

	if sub.Creds != nil && *sub.Creds != "" {
		sb.WriteString("\n🔗 <b>Ссылка для подключения:</b>\n")
		fmt.Fprintf(&sb, "<code>%s</code>", html.EscapeString(*sub.Creds))
	} else {
		sb.WriteString("\n⏳ Готовим подключение. Придёт отдельным сообщением через пару секунд.")
	}

	rows := make([][]map[string]any, 0, 3)
	if b.cfg.InstructionURL != "" {
		rows = append(rows, []map[string]any{iconURLBtn("Инструкция", b.cfg.InstructionURL, iconInstr)})
	}
	rows = append(rows,
		[]map[string]any{iconCallbackBtn("Продлить", string(actRenewPickDuration), sub.ID, iconBuy)},
		[]map[string]any{iconCallbackBtn("В меню", string(actBackToMain), "", iconHome)},
	)
	return b.sendOrEditIconKeyboard(c, sb.String(), nil, iconKeyboard(rows...))
}

func (b *Bot) renderAbout(c tele.Context) error {
	text := b.cfg.AboutText
	if text == "" {
		text = "Wind-Walker VPN — быстрый и надёжный VPN-сервис."
	}
	kb := iconKeyboard(
		[]map[string]any{iconURLBtn("Политика конфиденциальности", "https://telegra.ph/Politika-konfidencialnosti-04-01-26", iconLock)},
		[]map[string]any{iconURLBtn("Пользовательское соглашение", "https://telegra.ph/Polzovatelskoe-soglashenie-04-01-19", iconAgreement)},
		[]map[string]any{iconCallbackBtn("В меню", string(actBackToMain), "", iconHome)},
	)
	return b.sendOrEditIconKeyboard(c, text, nil, kb)
}

func (b *Bot) renderSupport(c tele.Context) error {
	username := strings.TrimPrefix(b.cfg.SupportUsername, "@")
	text := fmt.Sprintf("По любым вопросам пиши @%s", username)
	kb := iconKeyboard(
		[]map[string]any{iconURLBtn("Открыть чат поддержки", "https://t.me/"+username, "")},
		[]map[string]any{iconCallbackBtn("В меню", string(actBackToMain), "", iconHome)},
	)
	return b.sendOrEditIconKeyboard(c, text, nil, kb)
}

func senderGreeting(c tele.Context) string {
	s := c.Sender()
	if s == nil {
		return "друг"
	}
	if s.FirstName != "" {
		return s.FirstName
	}
	if s.Username != "" {
		return "@" + s.Username
	}
	return "друг"
}
