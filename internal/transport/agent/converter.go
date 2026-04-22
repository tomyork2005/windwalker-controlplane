package agent

import (
	controlpb "control-plane/api/control"
	"control-plane/internal/model"
	"control-plane/internal/service"
	"fmt"

	"google.golang.org/protobuf/types/known/timestamppb"
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
		Version:     hello.GetVersion(),
		DriverTypes: hello.GetDriverTypes(),
	}
}
func operationToProto(o model.Operation) (*controlpb.ControlToAgent, error) {
	if err := o.Validate(); err != nil {
		return nil, fmt.Errorf("operation validate: %w", err)
	}

	meta := &controlpb.TaskMeta{
		RequestId: o.RequestID,
		Seq:       o.Seq,
	}

	task := &controlpb.Task{
		Meta: meta,
	}

	switch o.Kind {
	case model.OpUpsert:
		if o.Upsert == nil {
			return nil, fmt.Errorf("upsert payload is required for kind=%s", o.Kind)
		}
		task.Body = &controlpb.Task_Upsert{
			Upsert: &controlpb.UserUpsertRequest{
				User: &controlpb.User{
					Id:         o.Upsert.UserID,
					DriverType: o.Upsert.DriverType,
					ExpiresAt:  timestamppb.New(o.Upsert.ExpiresAt),
				},
			},
		}
	case model.OpRemove:
		if o.Remove == nil {
			return nil, fmt.Errorf("remove payload is required for kind=%s", o.Kind)
		}
		task.Body = &controlpb.Task_Remove{
			Remove: &controlpb.UserRemoveRequest{
				UserId:     o.Remove.UserID,
				DriverType: o.Remove.DriverType,
			},
		}
	case model.OpStatsAll:
		task.Body = &controlpb.Task_StatsAll{
			StatsAll: &controlpb.StatsAllRequest{},
		}
	case model.OpStatsUser:
		if o.StatsUser == nil {
			return nil, fmt.Errorf("stats_user payload is required for kind=%s", o.Kind)
		}
		task.Body = &controlpb.Task_StatsUser{
			StatsUser: &controlpb.StatsUserRequest{
				UserId: o.StatsUser.UserID,
			},
		}
	case model.OpHello:
		return &controlpb.ControlToAgent{
			Msg: &controlpb.ControlToAgent_Welcome{
				Welcome: &controlpb.Welcome{AgentId: o.AgentID, Message: "hello"},
			}}, nil
	default:
		return nil, fmt.Errorf("unsupported operation kind: %s", o.Kind)
	}

	return &controlpb.ControlToAgent{
		Msg: &controlpb.ControlToAgent_Task{
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
			UserID:   creds.GetUserId(),
			UUID:     v.GetUuid(),
			Host:     v.GetHost(),
			Port:     v.GetPort(),
			Security: v.GetSecurity(),
			Sni:      v.GetSni(),
			Alpn:     v.GetAlpn(),
			Path:     v.GetPath(),
			Network:  v.GetNetwork(),
			Flow:     v.GetFlow(),
			URI:      v.GetUri(),
		}
	}

	return nil
}
