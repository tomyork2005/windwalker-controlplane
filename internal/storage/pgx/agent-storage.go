package pgx

import (
	"context"
	"control-plane/internal/domain"
)

func (s *Storage) UpsertAgent(ctx context.Context, agent domain.Agent) error {
	const query = `
        INSERT INTO users (id, tg_username)
        VALUES ($1, $2)
        ON CONFLICT (tg_username) DO UPDATE
            SET tg_username = EXCLUDED.tg_username
        RETURNING id;
    `
}
