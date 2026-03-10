package agent

import (
	"context"
	controlpb "control-plane/api/control"
	"control-plane/internal/model"
	"control-plane/internal/service"
	"errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"time"
)

type Service interface {
	RegisterAgent(ctx context.Context, agent *model.Agent) error
	Heartbeat(ctx context.Context, agentID string, uptimeSeconds uint32) error

	HandleStartUserSubscribeResponse(ctx context.Context, subscribeID string, creds service.VPNCreds) error
	HandleRemoveCallback(ctx context.Context) error
	HandleStatsAll(ctx context.Context) error
	HandleError(ctx context.Context) error
}

type Dispatcher interface {
	RecoverPending(ctx context.Context, agentID string, fromExclusive uint64, limit int) error
	HandleAck(ctx context.Context, agentID string, seq uint64) error
	HandleNack(ctx context.Context, agentID string, seq uint64, errMsg string) error
}

type Server struct {
	svc             Service
	dispatcher      Dispatcher
	hub             *Hub
	hbTTL           time.Duration
	outboxScanLimit int

	controlpb.UnimplementedControlPlaneServer
}

func NewServer(svc Service, dispatcher Dispatcher, hub *Hub, hbTTL time.Duration) *Server {
	return &Server{
		svc:             svc,
		hub:             hub,
		dispatcher:      dispatcher,
		hbTTL:           hbTTL,
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
	err = s.svc.RegisterAgent(ctx, agent)
	if err != nil {
		// todo route errors
	}

	agentSession := newSessionFromAgent(ctx, agent)
	if old := s.hub.addAgentOrSwap(agentSession); old != nil {
		old.cancel()
		close(old.sendCh)
	}

	err = s.hub.TrySend(model.Operation{AgentID: agent.ID, Kind: model.OpHello})
	if err != nil {
		s.cleanupSession(agentSession)
		return status.Error(codes.Unavailable, "send buffer full")
	}

	// 2. Starting workers and send task what`s should be delivered, but agent was offline
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

	hbTimer := time.NewTimer(s.hbTTL)
	defer hbTimer.Stop()

	for {
		select {
		case in := <-inCh:
			resetTimer(hbTimer, s.hbTTL)

			switch x := in.Msg.(type) {
			case *controlpb.AgentToControl_Hb:
				_ = s.svc.Heartbeat(ctx, agent.ID, x.Hb.GetUptimeSeconds())

			case *controlpb.AgentToControl_Resp:
				seq, _ := s.handleResponse(ctx, agent.ID, x.Resp)
				agentSession.lastSeq.Store(seq)
			}

		case recvErr := <-recvErrCh:
			return s.finishStream(agentSession, sendErrCh, normalizeRecvErr(recvErr))

		case <-hbTimer.C:
			return s.finishStream(agentSession, sendErrCh, status.Error(codes.DeadlineExceeded, "heartbeat timeout"))
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

	from := ss.lastSeq.Load()
	if err := s.dispatcher.RecoverPending(ctx, ss.agentID, from, s.outboxScanLimit); err != nil {
		return
	}
}

func (s *Server) handleResponse(ctx context.Context, agentID string, resp *controlpb.Response) (uint64, error) {
	if resp == nil {
		return 0, status.Error(codes.InvalidArgument, "nil response")
	}

	meta := resp.GetMeta()
	if meta == nil {
		return 0, status.Error(codes.InvalidArgument, "response meta is required")
	}

	seq := meta.GetSeq()

	switch b := resp.Body.(type) {
	case *controlpb.Response_Upsert:
		if err := s.svc.HandleStartUserSubscribeResponse(ctx, meta.GetRequestId(), VPNCredsFromProto(b)); err != nil {
			_ = s.dispatcher.HandleNack(ctx, agentID, seq, err.Error())
			return seq, err
		}
		if err := s.dispatcher.HandleAck(ctx, agentID, seq); err != nil {
			return seq, err
		}
		return seq, nil

	case *controlpb.Response_StatsAll:
		if err := s.svc.HandleStatsAll(ctx); err != nil {
			_ = s.dispatcher.HandleNack(ctx, agentID, seq, err.Error())
			return seq, err
		}
		if err := s.dispatcher.HandleAck(ctx, agentID, seq); err != nil {
			return seq, err
		}
		return seq, nil

	case *controlpb.Response_Error:
		errMsg := "agent returned error"
		if b.Error != nil && b.Error.GetError() != "" {
			errMsg = b.Error.GetError()
		}
		if err := s.dispatcher.HandleNack(ctx, agentID, seq, errMsg); err != nil {
			return seq, err
		}
		if err := s.svc.HandleError(ctx); err != nil {
			return seq, err
		}
		return seq, nil

	default:
		return seq, nil
	}
}
