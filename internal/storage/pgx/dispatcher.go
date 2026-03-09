package pgx

import (
	"context"
	"control-plane/internal/model"
	"fmt"
	"github.com/georgysavva/scany/v2/pgxscan"
)

func (s *Storage) NextSeq(ctx context.Context, agentID string) (uint64, error) {
	const query = `
		INSERT INTO agent_seq (agent_id, last_seq)
		VALUES ($1, 1)
		ON CONFLICT (agent_id)
		DO UPDATE SET last_seq = agent_seq.last_seq + 1
		RETURNING last_seq
	`

	var seq uint64
	if err := s.getExecutor(ctx).QueryRow(ctx, query, agentID).Scan(&seq); err != nil {
		return 0, fmt.Errorf("next seq: %w", err)
	}

	return seq, nil
}

func (s *Storage) EnqueueTask(ctx context.Context, task *model.AgentTask) error {
	if task == nil {
		return fmt.Errorf("agent task is nil")
	}

	const query = `
		INSERT INTO agent_tasks (
			agent_id,
			seq,
			request_id,
			kind,
			payload
		) VALUES ($1, $2, $3, $4, $5)
	`

	_, err := s.getExecutor(ctx).Exec(
		ctx,
		query,
		task.AgentID,
		task.Seq,
		task.RequestID,
		task.Kind,
		task.Payload,
	)
	if err != nil {
		return fmt.Errorf("enqueue task: %w", err)
	}

	return nil
}

func (s *Storage) FetchPendingTasks(ctx context.Context, agentID string, fromExclusive uint64, retriesLimit int, limit int) ([]*model.AgentTask, error) {
	const query = `
		SELECT
			id,
			agent_id,
			seq,
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
		  AND seq > $2
		  AND done_at IS NULL
		  AND retries < $3
		ORDER BY seq ASC
		LIMIT $4
	`

	var rows []*model.AgentTask
	if err := pgxscan.Select(ctx, s.getExecutor(ctx), &rows, query, agentID, fromExclusive, retriesLimit, limit); err != nil {
		return nil, fmt.Errorf("fetch pending tasks: %w", err)
	}

	return rows, nil
}

func (s *Storage) MarkTaskSent(ctx context.Context, agentID string, seq uint64) error {
	const query = `
		UPDATE agent_tasks
		SET
			sent_at = COALESCE(sent_at, now()),
			retries = retries + 1
		WHERE agent_id = $1
		  AND seq = $2
		  AND done_at IS NULL
	`

	_, err := s.getExecutor(ctx).Exec(ctx, query, agentID, seq)
	if err != nil {
		return fmt.Errorf("mark task sent: %w", err)
	}

	return nil
}

func (s *Storage) MarkTaskAck(ctx context.Context, agentID string, seq uint64) error {
	const query = `
		UPDATE agent_tasks
		SET
			done_at = now(),
			ok = TRUE,
			error = ''
		WHERE agent_id = $1
		  AND seq = $2
		  AND done_at IS NULL
	`

	_, err := s.getExecutor(ctx).Exec(ctx, query, agentID, seq)
	if err != nil {
		return fmt.Errorf("mark task ack: %w", err)
	}

	return nil
}

func (s *Storage) MarkTaskNack(ctx context.Context, agentID string, seq uint64, errMsg string) error {
	const query = `
		UPDATE agent_tasks
		SET
			done_at = now(),
			ok = FALSE,
			error = $3
		WHERE agent_id = $1
		  AND seq = $2
		  AND done_at IS NULL
	`

	_, err := s.getExecutor(ctx).Exec(ctx, query, agentID, seq, errMsg)
	if err != nil {
		return fmt.Errorf("mark task nack: %w", err)
	}

	return nil
}

func (s *Storage) IncTaskRetries(ctx context.Context, agentID string, seq uint64, errMsg string) error {
	const query = `
		UPDATE agent_tasks
		SET
			retries = retries + 1,
			error = $3
		WHERE agent_id = $1
		  AND seq = $2
		  AND done_at IS NULL
	`

	_, err := s.getExecutor(ctx).Exec(ctx, query, agentID, seq, errMsg)
	if err != nil {
		return fmt.Errorf("inc task retries: %w", err)
	}

	return nil
}
