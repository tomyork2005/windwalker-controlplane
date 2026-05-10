package agent

import (
	controlpb "control-plane/api/control"
	"control-plane/internal/model"
	"control-plane/internal/service"
	"fmt"
)

func agentFromHelloPb(hello *controlpb.AgentHello) *model.Agent {
	id := hello.GetAgentId()
	if id == "" {
		id = hello.GetInstanceId()
	}
	return &model.Agent{
		ID:          id,
		InstanceID:  hello.GetInstanceId(),
		Region:      hello.GetRegion(),
		DriverTypes: hello.GetDriverTypes(),
	}
}

func operationToProto(o model.Operation) (*controlpb.ControlToAgent, error) {
	if err := o.Validate(); err != nil {
		return nil, fmt.Errorf("operation validate: %w", err)
	}

	if o.Kind == model.OpHello {
		return &controlpb.ControlToAgent{
			Message: &controlpb.ControlToAgent_Welcome{
				Welcome: &controlpb.Welcome{AgentId: o.AgentID, Message: "hello"},
			},
		}, nil
	}

	task := &controlpb.Task{
		RequestId: o.RequestID,
	}

	switch o.Kind {
	case model.OpUpsert:
		if o.Upsert == nil {
			return nil, fmt.Errorf("upsert payload is required for kind=%s", o.Kind)
		}
		task.Body = &controlpb.Task_Upsert{
			Upsert: &controlpb.UserUpsertRequest{
				User: &controlpb.User{
					UserId:     o.Upsert.UserID,
					DriverType: o.Upsert.DriverType,
				},
			},
		}
	case model.OpRemove:
		if o.Remove == nil {
			return nil, fmt.Errorf("remove payload is required for kind=%s", o.Kind)
		}
		task.Body = &controlpb.Task_Remove{
			Remove: &controlpb.UserRemoveRequest{
				User: &controlpb.User{
					UserId:     o.Remove.UserID,
					DriverType: o.Remove.DriverType,
				},
			},
		}
	default:
		return nil, fmt.Errorf("unsupported operation kind: %s", o.Kind)
	}

	return &controlpb.ControlToAgent{
		Message: &controlpb.ControlToAgent_Task{
			Task: task,
		},
	}, nil
}

func VPNCredsFromProto(upsert *controlpb.Response_Upsert) service.VPNCreds {
	if upsert == nil || upsert.Upsert == nil {
		return nil
	}

	creds := upsert.Upsert.GetCreds()
	if creds == nil {
		return nil
	}

	if v := creds.GetVless(); v != nil {
		return &model.VlessCreds{
			UserID: creds.GetUserId(),
			URI:    v.GetUri(),
		}
	}

	return nil
}

func StatsFromProto(s *controlpb.Stats) model.AgentStats {
	if s == nil {
		return model.AgentStats{}
	}

	out := model.AgentStats{
		AgentID:       s.GetAgentId(),
		UptimeSeconds: s.GetUptimeSeconds(),
		WindowEnd:     s.GetWindowEnd().AsTime(),
		Users:         make([]model.UserUsageRow, 0, len(s.GetUsers())),
	}
	for _, u := range s.GetUsers() {
		out.Users = append(out.Users, model.UserUsageRow{
			UserID:    u.GetUserId(),
			BytesUp:   u.GetBytesUp(),
			BytesDown: u.GetBytesDown(),
			IPCount:   u.GetIpCount(),
		})
	}
	return out
}
