package agent

import (
	controlpb "control-plane/api/control"
	"fmt"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

type Agent struct {
	ID          string
	InstanceID  string
	Region      string
	Version     string
	DriverTypes []string
}

func (a *Agent) Validate() error {
	if a.ID == "" {
		return fmt.Errorf("agent_id is required")
	}
	if a.Region == "" {
		return fmt.Errorf("region is required")
	}
	if len(a.DriverTypes) == 0 {
		return fmt.Errorf("driver_types must not be empty")
	}
	return nil
}

func fromPBHello(pb *controlpb.AgentHello) Agent {
	return Agent{
		ID:          pb.GetAgentId(),
		InstanceID:  pb.GetInstanceId(),
		Region:      pb.GetRegion(),
		Version:     pb.GetVersion(),
		DriverTypes: pb.GetDriverTypes(),
	}
}

type UserUpsertInput struct {
	UserID        string
	Region        string
	DriverType    string
	SubscribeTime time.Time

	RequestID string
}

func (i *UserUpsertInput) Validate() error {
	if i.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	if i.UserID == "" {
		return fmt.Errorf("user_id is required")
	}
	if i.Region == "" {
		return fmt.Errorf("region is required")
	}
	if i.DriverType == "" {
		return fmt.Errorf("driver_type is required")
	}
	if i.SubscribeTime.Before(time.Now().Add(-1 * time.Second)) {
		return fmt.Errorf("subscribeTime is in the past")
	}
	return nil
}

type RemoveUserInput struct {
	UserID     string
	DriverType string

	RequestID string
}

func (i *RemoveUserInput) Validate() error {
	if i.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	if i.UserID == "" {
		return fmt.Errorf("user_id is required")
	}
	if i.DriverType == "" {
		return fmt.Errorf("driver_type is required")
	}
	return nil
}

type Task struct {
	AgentID   string
	RequestID string
	Seq       uint64
	Type      string

	UserID        string
	DriverType    string
	Region        string
	SubscribeTime time.Time
}

func (t *Task) Validate() error {
	if t.AgentID == "" {
		return fmt.Errorf("agent_id is required")
	}
	if t.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	if t.Seq == 0 {
		return fmt.Errorf("seq is required")
	}
	if t.Type != taskUpsertUser && t.Type != taskRemoveUser {
		return fmt.Errorf("operation type is invalid")
	}
	if t.UserID == "" {
		return fmt.Errorf("user_id is required")
	}
	return nil
}

func (t *Task) ToProto() *controlpb.ControlToAgent_Task {

	meta := &controlpb.TaskMeta{
		RequestId: t.RequestID,
		Seq:       t.Seq,
	}
	switch t.Type {
	case taskUpsertUser:
		return &controlpb.ControlToAgent_Task{
			Task: &controlpb.Task{
				Meta: meta,
				Body: &controlpb.Task_Upsert{
					Upsert: &controlpb.UpsertUser{
						User: &controlpb.User{
							Id:         t.UserID,
							Name:       t.UserID,
							DriverType: t.DriverType,
							ExpiresAt:  timestamppb.New(t.SubscribeTime),
						},
					},
				},
			},
		}
	case taskRemoveUser:
		return &controlpb.ControlToAgent_Task{
			Task: &controlpb.Task{
				Meta: meta,
				Body: &controlpb.Task_Remove{
					Remove: &controlpb.RemoveUser{
						UserId:     t.UserID,
						DriverType: t.DriverType,
					},
				},
			},
		}
	default:
		return nil
	}
}

type OutboxTask struct {
	AgentID       string
	Seq           uint64
	RequestID     string
	OperationKind string
	Payload       []byte
}
