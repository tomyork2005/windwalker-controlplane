package service

import (
	"context"
	"control-plane/internal/model"
	"encoding/json"
	"fmt"
	"log/slog"
)

const retriesLimit = 10

type DispatchStorage interface {
	EnqueueTask(ctx context.Context, task *model.AgentTask) (int64, error)
	FetchPendingTasks(ctx context.Context, agentID string, fromExclusiveID int64, retriesLimit int, limit int) ([]*model.AgentTask, error)

	MarkTaskSent(ctx context.Context, taskID int64) error
	MarkTaskAck(ctx context.Context, agentID string, requestID string) error
	MarkTaskNack(ctx context.Context, agentID string, requestID string, errMsg string) error
	IncTaskRetries(ctx context.Context, taskID int64, errMsg string) error

	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type DispatcherSender interface {
	TrySend(op model.Operation) error
}

type Dispatcher struct {
	store  DispatchStorage
	sender DispatcherSender
}

func NewDispatcher(store DispatchStorage, transport DispatcherSender) *Dispatcher {
	return &Dispatcher{
		store:  store,
		sender: transport,
	}
}

func (d *Dispatcher) DispatchUpsert(ctx context.Context, agentID string, subscriptionID string, up *model.AgentUpsertPayload) (*model.Operation, error) {
	payload, err := json.Marshal(up)
	if err != nil {
		return nil, fmt.Errorf("marshal upsert payload: %w", err)
	}

	taskID, err := d.store.EnqueueTask(ctx, &model.AgentTask{
		AgentID:   agentID,
		RequestID: subscriptionID,
		Kind:      model.OpUpsert,
		Payload:   payload,
	})
	if err != nil {
		return nil, fmt.Errorf("enqueue upsert task: %w", err)
	}

	return &model.Operation{
		AgentID:   agentID,
		TaskID:    taskID,
		RequestID: subscriptionID,
		Kind:      model.OpUpsert,
		Upsert:    up,
	}, nil
}

func (d *Dispatcher) DispatchRemove(ctx context.Context, agentID string, requestID string, rm *model.AgentRemovePayload) error {
	payload, err := json.Marshal(rm)
	if err != nil {
		return fmt.Errorf("marshal remove payload: %w", err)
	}

	taskID, err := d.store.EnqueueTask(ctx, &model.AgentTask{
		AgentID:   agentID,
		RequestID: requestID,
		Kind:      model.OpRemove,
		Payload:   payload,
	})
	if err != nil {
		return fmt.Errorf("enqueue remove task: %w", err)
	}

	op := model.Operation{
		AgentID:   agentID,
		TaskID:    taskID,
		RequestID: requestID,
		Kind:      model.OpRemove,
		Remove:    rm,
	}

	d.trySend(ctx, op)
	return nil
}

func (d *Dispatcher) TryDispatchPrepared(ctx context.Context, op *model.Operation) {
	d.trySend(ctx, *op)
}

func (d *Dispatcher) RecoverPending(ctx context.Context, agentID string, fromExclusiveID int64, limit int) error {
	from := fromExclusiveID

	for {
		tasks, err := d.store.FetchPendingTasks(ctx, agentID, from, retriesLimit, limit)
		if err != nil {
			return fmt.Errorf("fetch pending tasks: %w", err)
		}

		if len(tasks) == 0 {
			return nil
		}

		for _, task := range tasks {
			op, err := model.TaskToOperation(task)
			if err != nil {
				continue
			}

			if err = d.sender.TrySend(op); err != nil {
				if incErr := d.store.IncTaskRetries(ctx, task.ID, err.Error()); incErr != nil {
					return fmt.Errorf("inc task retries: %w", incErr)
				}
				from = task.ID
				continue
			}

			if err := d.store.MarkTaskSent(ctx, task.ID); err != nil {
				return fmt.Errorf("mark task sent: %w", err)
			}

			from = task.ID
		}

		if len(tasks) < limit {
			return nil
		}
	}
}

func (d *Dispatcher) HandleAck(ctx context.Context, agentID string, requestID string) error {
	return d.store.MarkTaskAck(ctx, agentID, requestID)
}

func (d *Dispatcher) HandleNack(ctx context.Context, agentID string, requestID string, errMsg string) error {
	return d.store.MarkTaskNack(ctx, agentID, requestID, errMsg)
}

func (d *Dispatcher) trySend(ctx context.Context, op model.Operation) {
	if err := d.sender.TrySend(op); err != nil {
		slog.Warn("dispatcher trySend failed", "agent_id", op.AgentID, "request_id", op.RequestID, "kind", op.Kind, "err", err)
		return
	}

	if op.TaskID == 0 {
		return
	}
	if err := d.store.MarkTaskSent(ctx, op.TaskID); err != nil {
		slog.Error("mark task sent after trySend", "task_id", op.TaskID, "err", err)
	}
}
