package bot

import (
	"context"
	"fmt"
	tele "gopkg.in/telebot.v4"
)

func (b *Bot) Send(ctx context.Context, chatID int64, message string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("telegram send canceled: %w", err)
	}

	recipient := &tele.Chat{ID: chatID}
	_, err := b.bot.Send(recipient, message, tele.ModeHTML)
	if err != nil {
		return fmt.Errorf("telegram send to chat_id=%d: %w", chatID, err)
	}

	return nil
}
