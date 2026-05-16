package agent

import (
	"context"
	controlpb "control-plane/api/control"
	"control-plane/internal/model"
	"control-plane/internal/service"
	"errors"
	"io"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service interface {
	RegisterAgent(ctx context.Context, agent *model.Agent) error
	HandleStats(ctx context.Context, stats model.AgentStats) error
	HandleStartUserSubscribeResponse(ctx context.Context, subscribeID string, creds service.VPNCreds) error
}

type Dispatcher interface {
	RecoverPending(ctx context.Context, agentID string, fromExclusiveID int64, limit int) error
	HandleAck(ctx context.Context, agentID string, requestID string) error
	HandleNack(ctx context.Context, agentID string, requestID string, errMsg string) error
}

type Server struct {
	svc             Service
	dispatcher      Dispatcher
	hub             *Hub
	statsTTL        time.Duration
	outboxScanLimit int

	controlpb.UnimplementedControlPlaneServer
}

func NewServer(svc Service, dispatcher Dispatcher, hub *Hub, statsTTL time.Duration) *Server {
	return &Server{
		svc:             svc,
		hub:             hub,
		dispatcher:      dispatcher,
		statsTTL:        statsTTL,
		outboxScanLimit: 256,
	}
}

var _ controlpb.ControlPlaneServer = (*Server)(nil)

func (s *Server) Workstream(stream controlpb.ControlPlane_WorkstreamServer) error {
	ctx := stream.Context()

	// 1. Register agent
	first, err := stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return status.Error(codes.InvalidArgument, "empty stream")
		}
		return err
	}
	hello := first.GetHello()
	if hello == nil {
		return status.Error(codes.InvalidArgument, "first message must be AgentHello")
	}

	agent := agentFromHelloPb(hello)
	if err = s.svc.RegisterAgent(ctx, agent); err != nil {
		return status.Errorf(codes.Internal, "register agent: %v", err)
	}

	agentSession := newSessionFromAgent(ctx, agent)
	if old := s.hub.addAgentOrSwap(agentSession); old != nil {
		old.cancel()
		close(old.sendCh)
	}

	if err = s.hub.TrySend(model.Operation{AgentID: agent.ID, Kind: model.OpHello}); err != nil {
		s.cleanupSession(agentSession)
		return status.Error(codes.Unavailable, "send buffer full")
	}

	// 2. Recover pending tasks from previous offline window
	agentSession.recovering.Store(true)
	go s.flushPending(agentSession.ctx, agentSession)

	sendErrCh := make(chan error, 1)
	go func() { sendErrCh <- sender(agentSession.ctx, stream, agentSession) }()

	inCh := make(chan *controlpb.AgentToControl, 1)
	recvErrCh := make(chan error, 1)
	go func() {
		for {
			in, err := stream.Recv()
			if err != nil {
				recvErrCh <- err
				return
			}
			inCh <- in
		}
	}()

	statsTimer := time.NewTimer(s.statsTTL)
	defer statsTimer.Stop()

	for {
		select {
		case in := <-inCh:
			resetTimer(statsTimer, s.statsTTL)

			switch x := in.Msg.(type) {
			case *controlpb.AgentToControl_Stats:
				if err := s.svc.HandleStats(ctx, StatsFromProto(x.Stats)); err != nil {
					slog.Error("handle stats failed", "agent_id", agent.ID, "err", err)
				}

			case *controlpb.AgentToControl_Resp:
				if err := s.handleResponse(ctx, agent.ID, x.Resp); err != nil {
					slog.Error("handle response failed", "agent_id", agent.ID, "err", err)
				}
			}

		case recvErr := <-recvErrCh:
			return s.finishStream(agentSession, sendErrCh, normalizeRecvErr(recvErr))

		case sendErr := <-sendErrCh:
			return s.finishStream(agentSession, nil, sendErr)

		case <-statsTimer.C:
			return s.finishStream(agentSession, sendErrCh, status.Error(codes.DeadlineExceeded, "stats timeout"))
		}
	}
}

func (s *Server) finishStream(ss *session, sendErrCh <-chan error, cause error) error {
	s.cleanupSession(ss)

	var sendErr error
	if sendErrCh != nil {
		select {
		case sendErr = <-sendErrCh:
		default:
		}
	}

	if cause != nil {
		return cause
	}
	if sendErr != nil {
		return sendErr
	}
	return nil
}

func normalizeRecvErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func sender(ctx context.Context, stream controlpb.ControlPlane_WorkstreamServer, s *session) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case operation, ok := <-s.sendCh:
			if !ok {
				return nil
			}
			out, err := operationToProto(operation)
			if err != nil {
				return err
			}
			if err = stream.Send(out); err != nil {
				return err
			}
		}
	}
}

func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	_ = t.Reset(d)
}

func (s *Server) cleanupSession(ss *session) {
	ss.cancel()
	s.hub.remove(ss.agentID)
	close(ss.sendCh)
}

func (s *Server) flushPending(ctx context.Context, ss *session) {
	defer ss.recovering.Store(false)

	if err := s.dispatcher.RecoverPending(ctx, ss.agentID, 0, s.outboxScanLimit); err != nil {
		slog.Error("recover pending failed", "agent_id", ss.agentID, "err", err)
		return
	}
}

func (s *Server) handleResponse(ctx context.Context, agentID string, resp *controlpb.Response) error {
	if resp == nil {
		return status.Error(codes.InvalidArgument, "nil response")
	}

	reqID := resp.GetRequestId()
	if reqID == "" {
		slog.Error("agent echoed empty request_id — cannot route response", "agent_id", agentID)
		return nil
	}

	switch b := resp.Body.(type) {
	case *controlpb.Response_Upsert:
		subID := model.SubscriptionIDFromRequestID(reqID)
		if err := s.svc.HandleStartUserSubscribeResponse(ctx, subID, VPNCredsFromProto(b)); err != nil {
			slog.Error("upsert response handling failed — task left pending for recovery",
				"agent_id", agentID, "request_id", reqID, "err", err)
			return err
		}
		return s.dispatcher.HandleAck(ctx, agentID, reqID)

	case *controlpb.Response_Remove:
		slog.Info("agent confirmed user removal", "agent_id", agentID, "request_id", reqID)
		return s.dispatcher.HandleAck(ctx, agentID, reqID)

	case *controlpb.Response_Error:
		errMsg := b.Error
		if errMsg == "" {
			errMsg = "agent returned error"
		}
		return s.dispatcher.HandleNack(ctx, agentID, reqID, errMsg)

	default:
		slog.Warn("unknown response body type", "agent_id", agentID, "request_id", reqID)
		return nil
	}
}
