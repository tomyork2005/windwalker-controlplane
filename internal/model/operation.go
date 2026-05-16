package model

import (
	"fmt"
	"strings"
)

type OperationKind string

const (
	OpUpsert OperationKind = "upsert"
	OpRemove OperationKind = "remove"
	OpHello  OperationKind = "hello"
)

func AgentRequestID(subscriptionID string, kind OperationKind) string {
	return subscriptionID + ":" + string(kind)
}

func SubscriptionIDFromRequestID(requestID string) string {
	idx := strings.LastIndex(requestID, ":")
	if idx < 0 {
		return requestID
	}
	switch OperationKind(requestID[idx+1:]) {
	case OpUpsert, OpRemove:
		return requestID[:idx]
	default:
		return requestID
	}
}

type Operation struct {
	AgentID   string
	RequestID string
	TaskID    int64
	Kind      OperationKind

	Upsert *AgentUpsertPayload
	Remove *AgentRemovePayload
}

type AgentUpsertPayload struct {
	UserID     string `json:"user_id"`
	DriverType string `json:"driver"`
}

type AgentRemovePayload struct {
	UserID     string `json:"user_id"`
	DriverType string `json:"driver"`
}

func (o *Operation) Validate() error {
	if o.AgentID == "" {
		return fmt.Errorf("agent_id is required")
	}
	if o.Kind == OpHello {
		return nil
	}

	if o.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}

	return nil
}
