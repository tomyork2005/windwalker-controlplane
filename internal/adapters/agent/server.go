package agent

import (
	"context"
	controlpb "control-plane/api/control"
	"control-plane/internal/domain"
	"errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"time"
)

type service interface {
	UpsertAgent(ctx context.Context, agent domain.Agent) error

	Ack(ctx context.Context, agentID string, seq uint64) error
	Nack(ctx context.Context, agentID string, seq uint64, err string) error
	Heartbeat(ctx context.Context, agentID string, uptimeSeconds uint32) error
}

type outbox interface {
	FlushPending(ctx context.Context, agentID string, fromExclusive uint64, limit int) (wires []*controlpb.ControlToAgent, maxSeq uint64, err error)
	MarkTaskSent(ctx context.Context, agentID string, seq uint64) error
}

type Server struct {
	svc             service
	outbox          outbox
	reg             *registry
	hbTTL           time.Duration
	outboxScanLimit int
}

func NewManager(svc service, reg *registry, outbox outbox, hbTTL time.Duration) *Server {
	return &Server{
		svc:             svc,
		reg:             reg,
		outbox:          outbox,
		hbTTL:           hbTTL,
		outboxScanLimit: 256,
	}
}

var _ controlpb.ControlPlaneServer = (*Server)(nil)

func (s *Server) Workstream(stream controlpb.ControlPlane_WorkstreamServer) error {
	ctx := stream.Context()

	// Receive hello from vpn-agent / create agent
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
	agent := fromPBHelloToAgent(hello)

	// Added agent to service and registry
	if err := s.svc.UpsertAgent(ctx, agent); err != nil {
		return status.Errorf(codes.Internal, "handshake: %v", err)
	}

	sessCtx, cancel := context.WithCancel(ctx)
	ss := &session{
		agentID:     agent.ID,
		region:      agent.Region,
		driverTypes: toSet(agent.DriverTypes),
		sendCh:      make(chan Operation, 1024),
		cancel:      cancel,
	}
	if old := s.reg.addAgentOrSwap(ss); old != nil {
		old.cancel()
		close(old.sendCh)
	}

	// Send Welcome
	if !trySend(ss, &controlpb.ControlToAgent{
		Msg: &controlpb.ControlToAgent_Welcome{
			Welcome: &controlpb.Welcome{AgentId: agent.ID, Message: "hello"},
		},
	}) {
		s.cleanupSession(ss)
		return status.Error(codes.Unavailable, "send buffer full")
	}

	// Starting to send tasks that have been added when agent offline
	ss.recovering.Store(true)
	go s.flushPending(sessCtx, ss)

	// Starting workers
	sendErrCh := make(chan error, 1)
	go func() { sendErrCh <- sender(sessCtx, stream, ss) }()

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
			case *controlpb.AgentToControl_Heartbeat:
				_ = s.svc.Heartbeat(ctx, agent.ID, x.Heartbeat.GetUptimeSeconds())

			case *controlpb.AgentToControl_Ack:
				_ = s.svc.Ack(ctx, agent.ID, x.Ack.GetSeq())
				ss.lastAck.Store(x.Ack.GetSeq())

			case *controlpb.AgentToControl_Nack:
				_ = s.svc.Nack(ctx, agent.ID, x.Nack.GetSeq(), x.Nack.GetError())

			case *controlpb.AgentToControl_AllStats:

			case *controlpb.AgentToControl_UserStats:

			}

		case recvErr := <-recvErrCh:
			return s.finishStream(ss, sendErrCh, normalizeRecvErr(recvErr))

		case <-hbTimer.C:
			return s.finishStream(ss, sendErrCh, status.Error(codes.DeadlineExceeded, "heartbeat timeout"))
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
		case task, ok := <-s.sendCh:
			if !ok {
				return nil
			}
			out := task.ToProto()
			if err := stream.Send(&controlpb.ControlToAgent{Msg: out}); err != nil {
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

func trySend(s *session, msg *controlpb.ControlToAgent) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}() // if channel close -- recovering panic
	select {
	case s.sendCh <- msg:
		return true
	default:
		return false
	}
}

func (s *Server) cleanupSession(ss *session) {
	ss.cancel()
	s.reg.remove(ss.agentID)
	close(ss.sendCh)
}

func (s *Server) flushPending(ctx context.Context, ss *session) {
	from := ss.lastAck.Load()
	for {
		select {
		case <-ctx.Done():
			return
		default:

		}
		wires, maxSeq, err := s.outbox.FlushPending(ctx, ss.agentID, from, s.outboxScanLimit)
		if err != nil {
			return
		}
		if len(wires) == 0 {
			ss.recovering.Store(false)
			return
		}
		for _, w := range wires {
			if trySend(ss, w) {
				if t := w.GetTask(); t != nil {
					_ = s.outbox.MarkTaskSent(ctx, ss.agentID, t.Meta.Seq)
				}
			}
		}
		if maxSeq > from {
			from = maxSeq
		}
	}
}
