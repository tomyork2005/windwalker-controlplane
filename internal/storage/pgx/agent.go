package pgx

import (
	"context"
	"control-plane/internal/model"
	"errors"
	"fmt"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
)

// Receiver

func (s *Storage) UpsertAgent(ctx context.Context, agent *model.Agent) error {
	if agent == nil {
		return errors.New("agent is nil")
	}

	const query = `
		INSERT INTO agents (
			id,
			instance_id,
			region,
			driver_types
		)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET
			instance_id = EXCLUDED.instance_id,
			region = EXCLUDED.region,
			driver_types = EXCLUDED.driver_types
	`

	_, err := s.getExecutor(ctx).Exec(
		ctx,
		query,
		agent.ID,
		agent.InstanceID,
		agent.Region,
		agent.DriverTypes,
	)
	if err != nil {
		return fmt.Errorf("upsert agent: %w", err)
	}

	return nil
}

func (s *Storage) UpdateAgentStats(
	ctx context.Context,
	agentID string,
	uptime uint64,
	seenAt,
	deadline time.Time,
) error {
	const query = `
		UPDATE agents
		SET
			uptime_seconds = $2,
			last_seen_at = $3,
			stats_deadline_at = $4
		WHERE id = $1
	`

	tag, err := s.getExecutor(ctx).Exec(ctx, query, agentID, int64(uptime), seenAt, deadline)
	if err != nil {
		return fmt.Errorf("update agent stats: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update agent stats: agent not found")
	}

	return nil
}

func (s *Storage) ResolveChatIDBySubscribeID(ctx context.Context, subscribeID string) (int64, error) {
	const query = `
		SELECT chat_id
		FROM subscriptions
		WHERE id = $1
		LIMIT 1
	`

	var chatID int64
	if err := pgxscan.Get(ctx, s.getExecutor(ctx), &chatID, query, subscribeID); err != nil {
		return 0, fmt.Errorf("resolve chat id by subscribe id: %w", err)
	}

	return chatID, nil
}

func (s *Storage) StoreSubscriptionCreds(ctx context.Context, subscriptionID string, creds string) error {
	const query = `
		UPDATE subscriptions
		SET creds = $2, creds_ready_at = now()
		WHERE id = $1
	`

	tag, err := s.getExecutor(ctx).Exec(ctx, query, subscriptionID, creds)
	if err != nil {
		return fmt.Errorf("store subscription creds: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("store subscription creds: subscription %q not found", subscriptionID)
	}
	return nil
}

// Sender

func (s *Storage) ChooseBestAgent(ctx context.Context, driverType string, region string) (string, error) {
	const query = `
		SELECT a.id
		FROM agents a
		LEFT JOIN subscriptions sub
			ON sub.agent_id = a.id
			AND sub.status IN ('active', 'pending')
		WHERE a.region = $1
			AND $2 = ANY(a.driver_types)
			AND a.stats_deadline_at IS NOT NULL
			AND a.stats_deadline_at > now()
		GROUP BY a.id, a.last_seen_at
		ORDER BY COUNT(sub.id) ASC, a.last_seen_at DESC NULLS LAST, a.id ASC
		LIMIT 1
	`

	var agentID string
	if err := pgxscan.Get(ctx, s.getExecutor(ctx), &agentID, query, region, driverType); err != nil {
		return "", fmt.Errorf("choose best agent: %w", err)
	}

	return agentID, nil
}

func (s *Storage) BindSubscriptionToAgent(ctx context.Context, subscriptionID string, agentID string) error {
	const query = `
		UPDATE subscriptions
		SET agent_id = $2
		WHERE id = $1
	`

	tag, err := s.getExecutor(ctx).Exec(ctx, query, subscriptionID, agentID)
	if err != nil {
		return fmt.Errorf("bind subscription to agent: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("bind subscription to agent: subscription not found")
	}

	return nil
}
