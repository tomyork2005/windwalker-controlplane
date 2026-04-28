package bot

import (
	"fmt"

	tele "gopkg.in/telebot.v4"
)

// telebot v4 (latest beta) ещё не реализовал Bot API 9.4 поле
// icon_custom_emoji_id у InlineKeyboardButton, поэтому для меню-кнопок мы
// строим reply_markup как map[string]any и шлём через b.bot.Raw(...).

// iconCallbackBtn возвращает inline-кнопку, которая дёргает callback action
// (формат данных совпадает с tele.Btn.Data — "<unique>|<payload>" в одном поле
// callback_data; парсится в parseCallback). iconID — пустой = без custom icon.
func iconCallbackBtn(text, action, payload, iconID string) map[string]any {
	data := action
	if payload != "" {
		data = action + "|" + payload
	}
	btn := map[string]any{
		"text":          text,
		"callback_data": data,
	}
	if iconID != "" {
		btn["icon_custom_emoji_id"] = iconID
	}
	return btn
}

// iconURLBtn возвращает inline-кнопку, открывающую URL.
func iconURLBtn(text, url, iconID string) map[string]any {
	btn := map[string]any{
		"text": text,
		"url":  url,
	}
	if iconID != "" {
		btn["icon_custom_emoji_id"] = iconID
	}
	return btn
}

// iconKeyboard собирает finalized reply_markup из rows кнопок.
func iconKeyboard(rows ...[]map[string]any) map[string]any {
	return map[string]any{"inline_keyboard": rows}
}

// sendOrEditIconKeyboard рендерит сообщение с inline-keyboard, в которой
// каждая кнопка может иметь icon_custom_emoji_id. Поведение симметрично
// editOrSend: при callback пытается edit, при failure или новой команде шлёт
// новое сообщение. Если photo != nil — главное меню рендерится как photo+caption.
//
// При первом sendPhoto без кэша file_id мы вынуждены залить фото через
// telebot (без custom-keyboard, т.к. multipart-upload через Raw неудобен) и
// сразу патчим reply_markup отдельным editMessageReplyMarkup. После capture
// file_id'а все последующие отправки идут одним Raw "sendPhoto".
func (b *Bot) sendOrEditIconKeyboard(c tele.Context, text string, photo *tele.Photo, kb map[string]any) error {
	chatID := c.Chat().ID

	if c.Callback() != nil {
		msgID := c.Callback().Message.ID
		// Photo-сообщение → editMessageCaption.
		if _, err := b.bot.Raw("editMessageCaption", map[string]any{
			"chat_id":      chatID,
			"message_id":   msgID,
			"caption":      text,
			"parse_mode":   "HTML",
			"reply_markup": kb,
		}); err == nil {
			return nil
		}
		// Text-сообщение → editMessageText.
		if _, err := b.bot.Raw("editMessageText", map[string]any{
			"chat_id":      chatID,
			"message_id":   msgID,
			"text":         text,
			"parse_mode":   "HTML",
			"reply_markup": kb,
		}); err == nil {
			return nil
		}
		// Edit не сработал (старое сообщение удалили или сменился тип) —
		// fall through на send нового.
	}

	if photo != nil {
		if photo.FileID != "" {
			_, err := b.bot.Raw("sendPhoto", map[string]any{
				"chat_id":      chatID,
				"photo":        photo.FileID,
				"caption":      text,
				"parse_mode":   "HTML",
				"reply_markup": kb,
			})
			return err
		}

		sent, err := b.bot.Send(c.Recipient(), photo, tele.ModeHTML)
		if err == nil {
			b.photoCache.Capture(sent)
			if _, err2 := b.bot.Raw("editMessageReplyMarkup", map[string]any{
				"chat_id":      chatID,
				"message_id":   sent.ID,
				"reply_markup": kb,
			}); err2 != nil {
				return fmt.Errorf("apply reply_markup after photo upload: %w", err2)
			}
			return nil
		}
		// Photo upload упал — fall back на plain text.
	}

	_, err := b.bot.Raw("sendMessage", map[string]any{
		"chat_id":      chatID,
		"text":         text,
		"parse_mode":   "HTML",
		"reply_markup": kb,
	})
	return err
}
