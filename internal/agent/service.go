package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const (
	taskUpsertUser = "upsert"
	taskRemoveUser = "remove"
)

type storage interface {
	UpsertAgent(ctx context.Context, a Agent) error
	UpdateAgentHeartbeat(ctx context.Context, agentID string, uptime uint64, seenAt, deadline time.Time) error
	MarkTaskAck(ctx context.Context, agentID string, seq uint64) error
	MarkTaskNack(ctx context.Context, agentID string, seq uint64, errMsg string) error
	MarkTaskSent(ctx context.Context, agentID string, seq uint64) error

	ChooseBestAgent(ctx context.Context, driverType string, region string) (string, error)
	FindAgentIDByUserID(ctx context.Context, userID string) (string, error)
	NextSeq(ctx context.Context, agentID string) (uint64, error)
	EnqueueTask(ctx context.Context, task OutboxTask) error
}

type TxManager interface {
	WithTx(context.Context, func(ctx context.Context) error) error
}

type Clock interface {
	Now() time.Time
}

type Service struct {
	store storage
	clock Clock
	tx    TxManager
	reg   *registry

	hbTTL time.Duration
}

func NewService(store storage, reg *registry, clock Clock, hbTTL time.Duration) *Service {
	if hbTTL <= 0 {
		hbTTL = 60 * time.Second
	}

	return &Service{
		store: store,
		reg:   reg,
		clock: clock,
		hbTTL: hbTTL,
	}
}

// From agents

func (s *Service) UpsertAgent(ctx context.Context, agent Agent) error {
	if err := agent.Validate(); err != nil {
		return err
	}

	err := s.store.UpsertAgent(ctx, agent)
	if err != nil {
		return fmt.Errorf("fail update heartbeat: %w", err)
	}

	now := s.clock.Now()
	err = s.store.UpdateAgentHeartbeat(ctx, agent.ID, 0, now, now.Add(s.hbTTL))
	if err != nil {
		return fmt.Errorf("fail update heartbeat: %w", err)
	}

	return nil
}

func (s *Service) Heartbeat(ctx context.Context, agentID string, uptimeSeconds uint32) error {
	now := s.clock.Now()
	return s.store.UpdateAgentHeartbeat(ctx, agentID, uint64(uptimeSeconds), now, now.Add(s.hbTTL))
}

func (s *Service) Ack(ctx context.Context, agentID string, seq uint64) error {
	return s.store.MarkTaskAck(ctx, agentID, seq)
}

func (s *Service) Nack(ctx context.Context, agentID string, seq uint64, errMsg string) error {
	return s.store.MarkTaskNack(ctx, agentID, seq, errMsg)
}

// From other control-plane components

func (s *Service) UpsertUser(ctx context.Context, input UserUpsertInput) error {
	err := input.Validate()
	if err != nil {
		return fmt.Errorf("fail upsert user validation: %w", err)
	}

	agentID, err := s.store.ChooseBestAgent(ctx, input.DriverType, input.Region)
	if err != nil {
		return fmt.Errorf("fail upsert user storage: %w", err)
	}

	expiresAt := input.SubscribeTime.UntilOr(s.clock.Now())
	outboxPayload := struct {
		UserID     string    `json:"user_id"`
		DriverType string    `json:"driver"`
		ExpiresAt  time.Time `json:"expires_at"`
	}{
		UserID:     input.UserID,
		DriverType: input.DriverType,
		ExpiresAt:  expiresAt.UTC().Truncate(time.Second),
	}
	payload, _ := json.Marshal(outboxPayload)

	err = s.tx.WithTx(ctx, func(ctx context.Context) error {
		seq, err := s.store.NextSeq(ctx, agentID)
		if err != nil {
			return err
		}

		err = s.store.EnqueueTask(ctx, OutboxTask{
			AgentID:       agentID,
			Seq:           seq,
			RequestID:     input.RequestID,
			OperationKind: taskUpsertUser,
			Payload:       payload,
		})
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("fail upsert user storage: %w", err)
	}

	s.trySendNow(agentID, Task{
		AgentID:   agentID,
		RequestID: input.RequestID,
		Seq:       seq,
		Kind:      TaskUpsert,
		Payload:   payload,
	})

	return nil
}

func (s *Service) RemoveUser(ctx context.Context, input RemoveUserInput) error {
	err := input.Validate()
	if err != nil {
		return fmt.Errorf("fail remove user validation: %w", err)
	}

	agentID, err := s.store.FindAgentIDByUserID(ctx, input.UserID)
	if err != nil {
		return fmt.Errorf("fail remove user storage: %w", err)
	}

	outboxPayload := struct {
		UserID     string `json:"user_id"`
		DriverType string `json:"driver"`
	}{
		UserID:     input.UserID,
		DriverType: input.DriverType,
	}
	payload, _ := json.Marshal(outboxPayload)

	err = s.tx.WithTx(ctx, func(ctx context.Context) error {
		seq, err := s.store.NextSeq(ctx, agentID)
		if err != nil {
			return err
		}

		err = s.store.EnqueueTask(ctx, OutboxTask{
			AgentID:       agentID,
			Seq:           seq,
			RequestID:     input.RequestID,
			OperationKind: taskRemoveUser,
			Payload:       payload,
		})
		if err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("fail remove user storage: %w", err)
	}

	s.trySendNow(agentID, OutboxTask{})

	return nil
}

func (s *Service) trySendNow(agentID string, t OutboxTask) {
	if s.reg == nil {
		return
	}

	err := s.reg.sendByAgentID(agentID)
	if err != nil {
		return
	}
	wire := buildEnvelope(t) // конвертация OutboxTask -> внутренний "конверт" транспорта
	if trySend(sess, wire) { // твоя логика отправки в sendCh/стрим
		_ = s.store.MarkTaskSent(context.Background(), agentID, t.Seq)
	}
}
