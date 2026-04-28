package model

import (
	"encoding/json"
	"fmt"
	"time"
)

type AgentTask struct {
	ID        int64         `db:"id"`
	AgentID   string        `db:"agent_id"`
	Seq       uint64        `db:"seq"`
	RequestID string        `db:"request_id"`
	Kind      OperationKind `db:"kind"`
	Payload   []byte        `db:"payload"`

	SentAt    *time.Time `db:"sent_at"`
	DoneAt    *time.Time `db:"done_at"`
	OK        *bool      `db:"ok"`
	Error     string     `db:"error"`
	Retries   int        `db:"retries"`
	CreatedAt time.Time  `db:"created_at"`
}

func TaskToOperation(task *AgentTask) (Operation, error) {
	if task == nil {
		return Operation{}, fmt.Errorf("task is nil")
	}

	op := Operation{
		AgentID:   task.AgentID,
		Seq:       task.Seq,
		RequestID: task.RequestID,
		Kind:      task.Kind,
	}

	switch task.Kind {
	case OpUpsert:
		var payload AgentUpsertPayload
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			return Operation{}, fmt.Errorf("unmarshal upsert payload: %w", err)
		}
		op.Upsert = &payload
		return op, nil

	case OpRemove:
		var payload AgentRemovePayload
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			return Operation{}, fmt.Errorf("unmarshal remove payload: %w", err)
		}
		op.Remove = &payload
		return op, nil

	case OpRenew:
		var payload AgentRenewPayload
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			return Operation{}, fmt.Errorf("unmarshal renew payload: %w", err)
		}
		op.Renew = &payload
		return op, nil

	default:
		return Operation{}, fmt.Errorf("unknown task kind: %s", task.Kind)
	}
}
