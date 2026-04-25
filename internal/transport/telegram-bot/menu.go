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

const mainMenuText = `🌍 <b>Wind-Walker VPN</b> — быстрый и надёжный VPN-сервис со стабильным подключением

⚡ Высокая скорость подключения
🎁 Бесплатный пробный доступ на 3 дня
📱 Одна подписка для всех видов устройств
♾ Возможность смотреть YouTube без рекламы`

func (b *Bot) editOrSend(c tele.Context, text string, kb *tele.ReplyMarkup, opts ...interface{}) error {
	args := make([]interface{}, 0, len(opts)+1)
	if kb != nil {
		args = append(args, kb)
	}
	args = append(args, opts...)

	if c.Callback() != nil {
		// Photo messages must use editMessageCaption; text messages must use editMessageText.
		// Try caption first (main menu / any screen sent as photo), fall back to text edit.
		if err := c.EditCaption(text, args...); err == nil {
			return nil
		}
		if err := c.Edit(text, args...); err == nil {
			return nil
		}
	}

	photo := b.photoCache.Build(text)
	if photo == nil {
		return c.Send(text, args...)
	}
	msg, err := b.bot.Send(c.Recipient(), photo, args...)
	if err != nil {
		// photo upload failed — last resort, send as plain text so user sees something.
		return c.Send(text, args...)
	}
	b.photoCache.Capture(msg)
	return nil
}

func (b *Bot) renderMainMenu(ctx context.Context, c tele.Context) error {
	showTrial := true
	user, err := b.svc.UpsertUserByTelegramID(ctx, c.Sender().ID, c.Sender().Username)
	if err == nil {
		used, _ := b.svc.HasUsedTrial(ctx, user.ID)
		showTrial = !used
	}
	return b.editOrSend(c, mainMenuText, buildMainMenuKeyboard(showTrial), tele.ModeHTML)
}

func buildMainMenuKeyboard(showTrial bool) *tele.ReplyMarkup {
	var kb tele.ReplyMarkup
	rows := make([]tele.Row, 0, 4)
	if showTrial {
		rows = append(rows, kb.Row(kb.Data("🎁 Попробовать бесплатно", string(actTrialActivate), "")))
	}
	rows = append(rows,
		kb.Row(kb.Data("💳 Купить / Продлить", string(actMenuBuy), "")),
		kb.Row(kb.Data("👤 Моя подписка", string(actMySubscription), "")),
		kb.Row(
			kb.Data("ℹ О нас", string(actAbout), ""),
			kb.Data("💬 Поддержка", string(actSupport), ""),
		),
	)
	kb.Inline(rows...)
	return &kb
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
			var kb tele.ReplyMarkup
			rows := []tele.Row{
				kb.Row(kb.Data("🎁 Попробовать бесплатно", string(actTrialActivate), "")),
				kb.Row(kb.Data("💳 Купить подписку", string(actMenuBuy), "")),
				kb.Row(kb.Data("◀ В меню", string(actBackToMain), "")),
			}
			kb.Inline(rows...)
			return b.editOrSend(c, text, &kb, tele.ModeHTML)
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
		sb.WriteString("\n⏳ Доступ ещё готовится — придёт отдельным сообщением, как только агент его выдаст.")
	}

	var kb tele.ReplyMarkup
	rows := make([]tele.Row, 0, 3)
	if b.cfg.InstructionURL != "" {
		rows = append(rows, kb.Row(kb.URL("📖 Инструкция", b.cfg.InstructionURL)))
	}
	rows = append(rows,
		kb.Row(kb.Data("💳 Продлить", string(actMenuBuy), "")),
		kb.Row(kb.Data("◀ В меню", string(actBackToMain), "")),
	)
	kb.Inline(rows...)
	return b.editOrSend(c, sb.String(), &kb, tele.ModeHTML)
}

func (b *Bot) renderAbout(c tele.Context) error {
	text := b.cfg.AboutText
	if text == "" {
		text = "Wind-Walker VPN — быстрый и надёжный VPN-сервис."
	}
	var kb tele.ReplyMarkup
	kb.Inline(kb.Row(kb.Data("◀ В меню", string(actBackToMain), "")))
	return b.editOrSend(c, text, &kb)
}

func (b *Bot) renderSupport(c tele.Context) error {
	username := strings.TrimPrefix(b.cfg.SupportUsername, "@")
	text := fmt.Sprintf("По любым вопросам пиши @%s", username)
	var kb tele.ReplyMarkup
	kb.Inline(
		kb.Row(kb.URL("Открыть чат поддержки", "https://t.me/"+username)),
		kb.Row(kb.Data("◀ В меню", string(actBackToMain), "")),
	)
	return b.editOrSend(c, text, &kb)
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
