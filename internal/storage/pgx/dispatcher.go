package pgx

import (
	"context"
	"control-plane/internal/model"
	"errors"
	"fmt"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
)

func (s *Storage) EnqueueTask(ctx context.Context, task *model.AgentTask) (int64, error) {
	if task == nil {
		return 0, fmt.Errorf("agent task is nil")
	}

	const query = `
		INSERT INTO agent_tasks (
			agent_id,
			request_id,
			kind,
			payload
		) VALUES ($1, $2, $3, $4)
		ON CONFLICT (agent_id, request_id, kind) DO NOTHING
		RETURNING id
	`

	var id int64
	err := s.getExecutor(ctx).QueryRow(
		ctx,
		query,
		task.AgentID,
		task.RequestID,
		task.Kind,
		task.Payload,
	).Scan(&id)
	if err != nil {
		// pgx возвращает ErrNoRows если ON CONFLICT DO NOTHING не вставил строку.
		// Идемпотентный no-op — лезем за существующим id.
		if errors.Is(err, pgx.ErrNoRows) {
			const lookup = `
				SELECT id
				FROM agent_tasks
				WHERE agent_id = $1 AND request_id = $2 AND kind = $3
				LIMIT 1
			`
			if lookupErr := pgxscan.Get(ctx, s.getExecutor(ctx), &id, lookup, task.AgentID, task.RequestID, task.Kind); lookupErr != nil {
				return 0, fmt.Errorf("enqueue task lookup after conflict: %w", lookupErr)
			}
			return id, nil
		}
		return 0, fmt.Errorf("enqueue task: %w", err)
	}

	return id, nil
}

func (s *Storage) FetchPendingTasks(ctx context.Context, agentID string, fromExclusiveID int64, retriesLimit int, limit int) ([]*model.AgentTask, error) {
	const query = `
		SELECT
			id,
			agent_id,
			request_id,
			kind,
			payload,
			sent_at,
			done_at,
			ok,
			error,
			retries,
			created_at
		FROM agent_tasks
		WHERE agent_id = $1
		  AND id > $2
		  AND done_at IS NULL
		  AND retries < $3
		ORDER BY id ASC
		LIMIT $4
	`

	var rows []*model.AgentTask
	if err := pgxscan.Select(ctx, s.getExecutor(ctx), &rows, query, agentID, fromExclusiveID, retriesLimit, limit); err != nil {
		return nil, fmt.Errorf("fetch pending tasks: %w", err)
	}

	return rows, nil
}

func (s *Storage) MarkTaskSent(ctx context.Context, taskID int64) error {
	const query = `
		UPDATE agent_tasks
		SET
			sent_at = COALESCE(sent_at, now()),
			retries = retries + 1
		WHERE id = $1
		  AND done_at IS NULL
	`

	_, err := s.getExecutor(ctx).Exec(ctx, query, taskID)
	if err != nil {
		return fmt.Errorf("mark task sent: %w", err)
	}

	return nil
}

func (s *Storage) MarkTaskAck(ctx context.Context, agentID string, requestID string) error {
	const query = `
		UPDATE agent_tasks
		SET
			done_at = now(),
			ok = TRUE,
			error = ''
		WHERE id = (
			SELECT id FROM agent_tasks
			WHERE agent_id = $1 AND request_id = $2 AND done_at IS NULL
			ORDER BY id ASC
			LIMIT 1
		)
	`

	_, err := s.getExecutor(ctx).Exec(ctx, query, agentID, requestID)
	if err != nil {
		return fmt.Errorf("mark task ack: %w", err)
	}

	return nil
}

func (s *Storage) MarkTaskNack(ctx context.Context, agentID string, requestID string, errMsg string) error {
	const query = `
		UPDATE agent_tasks
		SET
			done_at = now(),
			ok = FALSE,
			error = $3
		WHERE id = (
			SELECT id FROM agent_tasks
			WHERE agent_id = $1 AND request_id = $2 AND done_at IS NULL
			ORDER BY id ASC
			LIMIT 1
		)
	`

	_, err := s.getExecutor(ctx).Exec(ctx, query, agentID, requestID, errMsg)
	if err != nil {
		return fmt.Errorf("mark task nack: %w", err)
	}

	return nil
}

func (s *Storage) IncTaskRetries(ctx context.Context, taskID int64, errMsg string) error {
	const query = `
		UPDATE agent_tasks
		SET
			retries = retries + 1,
			error = $2
		WHERE id = $1
		  AND done_at IS NULL
	`

	_, err := s.getExecutor(ctx).Exec(ctx, query, taskID, errMsg)
	if err != nil {
		return fmt.Errorf("inc task retries: %w", err)
	}

	return nil
}
