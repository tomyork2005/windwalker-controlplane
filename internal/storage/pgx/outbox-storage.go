package pgx

import (
	"context"
	"encoding/json"
)

// todo fecth with for update for parralells

func (s *Storage) Save(ctx context.Context, eventType string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	query := `INSERT INTO outbox (id, type, payload, created_at)
	VALUES (gen_random_uuid(), $1, $2, now())`

	_, err = s.getExecutor(ctx).Exec(ctx, query, eventType, raw)
	if err != nil {
		return err
	}

	return nil
}
