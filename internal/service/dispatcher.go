package service

import (
	"context"
	"control-plane/internal/model"
	"encoding/json"
	"fmt"
	"log"
)

const retriesLimit = 10

type DispatchStorage interface {
	NextSeq(ctx context.Context, agentID string) (uint64, error)
	EnqueueTask(ctx context.Context, task *model.AgentTask) error
	FetchPendingTasks(ctx context.Context, agentID string, fromExclusive uint64, retriesLimit int, limit int) ([]*model.AgentTask, error)

	MarkTaskSent(ctx context.Context, agentID string, seq uint64) error
	MarkTaskAck(ctx context.Context, agentID string, seq uint64) error
	MarkTaskNack(ctx context.Context, agentID string, seq uint64, errMsg string) error
	IncTaskRetries(ctx context.Context, agentID string, seq uint64, errMsg string) error

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

	var seq uint64
	seq, err = d.store.NextSeq(ctx, agentID)
	if err != nil {
		return nil, err
	}

	err = d.store.EnqueueTask(ctx, &model.AgentTask{
		AgentID:   agentID,
		Seq:       seq,
		RequestID: subscriptionID,
		Kind:      model.OpUpsert,
		Payload:   payload,
	})
	if err != nil {
		return nil, fmt.Errorf("enqueue upsert task: %w", err)
	}

	return &model.Operation{
		AgentID:   agentID,
		Seq:       seq,
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

	var seq uint64
	err = d.store.WithTx(ctx, func(ctx context.Context) error {
		seq, err = d.store.NextSeq(ctx, agentID)
		if err != nil {
			return err
		}

		return d.store.EnqueueTask(ctx, &model.AgentTask{
			AgentID:   agentID,
			Seq:       seq,
			RequestID: requestID,
			Kind:      model.OpRemove,
			Payload:   payload,
		})
	})
	if err != nil {
		return fmt.Errorf("enqueue remove task: %w", err)
	}

	op := model.Operation{
		AgentID:   agentID,
		Seq:       seq,
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

func (d *Dispatcher) RecoverPending(ctx context.Context, agentID string, fromExclusive uint64, limit int) error {
	from := fromExclusive

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
				if incErr := d.store.IncTaskRetries(ctx, task.AgentID, task.Seq, err.Error()); incErr != nil {
					return fmt.Errorf("inc task retries: %w", incErr)
				}
				continue
			}

			if err := d.store.MarkTaskSent(ctx, task.AgentID, task.Seq); err != nil {
				return fmt.Errorf("mark task sent: %w", err)
			}

			from = task.Seq
		}

		if len(tasks) < limit {
			return nil
		}
	}
}

func (d *Dispatcher) HandleAck(ctx context.Context, agentID string, seq uint64) error {
	return d.store.MarkTaskAck(ctx, agentID, seq)
}

func (d *Dispatcher) HandleNack(ctx context.Context, agentID string, seq uint64, errMsg string) error {
	return d.store.MarkTaskNack(ctx, agentID, seq, errMsg)
}

func (d *Dispatcher) trySend(ctx context.Context, op model.Operation) {
	if err := d.sender.TrySend(op); err != nil {
		log.Printf("fail trySend op=%+v err=%v", op, err)
		return
	}

	_ = d.store.MarkTaskSent(ctx, op.AgentID, op.Seq)
}
