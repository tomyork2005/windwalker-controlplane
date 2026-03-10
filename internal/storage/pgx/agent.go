package pgx

import (
	"context"
	"control-plane/internal/model"
	"errors"
	"fmt"
	"github.com/georgysavva/scany/v2/pgxscan"
	"time"
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
			version,
			driver_types
		)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET
			instance_id = EXCLUDED.instance_id,
			region = EXCLUDED.region,
			version = EXCLUDED.version,
			driver_types = EXCLUDED.driver_types
	`

	_, err := s.getExecutor(ctx).Exec(
		ctx,
		query,
		agent.ID,
		agent.InstanceID,
		agent.Region,
		agent.Version,
		agent.DriverTypes,
	)
	if err != nil {
		return fmt.Errorf("upsert agent: %w", err)
	}

	return nil
}

func (s *Storage) UpdateAgentHeartbeat(
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
			hb_deadline_at = $4
		WHERE id = $1
	`

	tag, err := s.getExecutor(ctx).Exec(ctx, query, agentID, int64(uptime), seenAt, deadline)
	if err != nil {
		return fmt.Errorf("update agent heartbeat: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update agent heartbeat: agent not found")
	}

	return nil
}

func (s *Storage) ResolveChatIDBySubscribeID(ctx context.Context, subscribeID string) (int64, error) {
	const query = `
		SELECT i.chat_id
		FROM subscriptions s
		JOIN invoices i ON i.id = s.invoice_id
		WHERE s.id = $1
		LIMIT 1
	`

	var chatID int64
	if err := pgxscan.Get(ctx, s.getExecutor(ctx), &chatID, query, subscribeID); err != nil {
		return 0, fmt.Errorf("resolve chat id by subscribe id: %w", err)
	}

	return chatID, nil
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
			AND a.hb_deadline_at IS NOT NULL
			AND a.hb_deadline_at > now()
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

func (s *Storage) FindAgentIDByUserID(ctx context.Context, userID string) (string, error) {
	const query = `
		SELECT agent_id
		FROM subscriptions
		WHERE user_id = $1
			AND status IN ('active', 'pending')
			AND agent_id IS NOT NULL
		ORDER BY start_at DESC
		LIMIT 1
	`

	var agentID string
	if err := pgxscan.Get(ctx, s.getExecutor(ctx), &agentID, query, userID); err != nil {
		return "", fmt.Errorf("find agent id by user id: %w", err)
	}

	return agentID, nil
}
