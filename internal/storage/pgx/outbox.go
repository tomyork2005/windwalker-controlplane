package pgx

import (
	"context"
	"control-plane/internal/model"
	"encoding/json"
	"fmt"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/google/uuid"
)

func (s *Storage) SaveOutboxEvent(ctx context.Context, eventType string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal outbox payload: %w", err)
	}

	const query = `
		INSERT INTO outbox_events (
			id,
			event_type,
			payload
		) VALUES ($1, $2, $3)
	`

	_, err = s.getExecutor(ctx).Exec(ctx, query, uuid.NewString(), eventType, raw)
	if err != nil {
		return fmt.Errorf("save outbox event: %w", err)
	}

	return nil
}

func (s *Storage) FetchUnprocessedOutboxEvents(ctx context.Context, limit int, attemptsLimit int) ([]model.OutboxEvent, error) {
	const query = `
		SELECT
			id,
			event_type,
			payload,
			attempts,
			error,
			created_at,
			processed_at
		FROM outbox_events
		WHERE processed_at IS NULL
		  AND attempts < $1
		ORDER BY created_at
		LIMIT $2;
	`

	var events []model.OutboxEvent
	if err := pgxscan.Select(ctx, s.getExecutor(ctx), &events, query, attemptsLimit, limit); err != nil {
		return nil, fmt.Errorf("fetch unprocessed outbox events: %w", err)
	}

	return events, nil
}

func (s *Storage) MarkOutboxEventProcessed(ctx context.Context, id string) error {
	const query = `
		UPDATE outbox_events
		SET
			processed_at = now(),
			error = ''
		WHERE id = $1
	`

	_, err := s.getExecutor(ctx).Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("mark outbox event processed: %w", err)
	}

	return nil
}

func (s *Storage) MarkOutboxEventFailed(ctx context.Context, id string, errMsg string) error {
	const query = `
		UPDATE outbox_events
		SET
			attempts = attempts + 1,
			error = $2
		WHERE id = $1
	`

	_, err := s.getExecutor(ctx).Exec(ctx, query, id, errMsg)
	if err != nil {
		return fmt.Errorf("mark outbox event failed: %w", err)
	}

	return nil
}
