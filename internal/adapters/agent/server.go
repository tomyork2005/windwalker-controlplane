package agent

import (
	"context"
	controlpb "control-plane/api/control"
	"errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"time"
)

type service interface {
	UpsertAgent(ctx context.Context, agent Agent) error

	Ack(ctx context.Context, agentID string, seq uint64) error
	Nack(ctx context.Context, agentID string, seq uint64, err string) error
	Heartbeat(ctx context.Context, agentID string, uptimeSeconds uint32) error

	FlushPending(ctx context.Context, agentID string, fromExclusive uint64, limit int) (wires []*controlpb.ControlToAgent, maxSeq uint64, err error)
	MarkTaskSent(ctx context.Context, agentID string, seq uint64) error
}

type Manager struct {
	svc             service
	reg             *registry
	hbTTL           time.Duration
	outboxScanLimit int
}

func NewManager(svc service, reg *registry, hbTTL time.Duration) *Manager {
	return &Manager{
		svc:             svc,
		reg:             reg,
		hbTTL:           hbTTL,
		outboxScanLimit: 256,
	}
}

var _ controlpb.ControlPlaneServer = (*Manager)(nil)

func (m *Manager) Workstream(stream controlpb.ControlPlane_WorkstreamServer) error {
	ctx := stream.Context()

	// Receive hello from vpn-agent
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
	agent := fromPBHello(hello)

	// Added to service and registry session (last-wins policy)
	if err := m.svc.UpsertAgent(ctx, agent); err != nil {
		return status.Errorf(codes.Internal, "handshake: %v", err)
	}

	sessCtx, cancel := context.WithCancel(ctx)
	s := &session{
		agentID:     agent.ID,
		region:      agent.Region,
		driverTypes: toSet(agent.DriverTypes),
		sendCh:      make(chan Operation, 1024),
		cancel:      cancel,
	}
	if old := m.reg.addAgentOrSwap(s); old != nil {
		old.cancel()
		close(old.sendCh)
	}

	// Send Welcome
	if !trySend(s, &controlpb.ControlToAgent{
		Msg: &controlpb.ControlToAgent_Welcome{
			Welcome: &controlpb.Welcome{AgentId: agent.ID, Message: "hello"},
		},
	}) {
		m.cleanupSession(s)
		return status.Error(codes.Unavailable, "send buffer full")
	}

	// Starting to send tasks that have been added when agent offline
	s.recovering.Store(true)
	go m.flushPending(sessCtx, s)

	// Starting send-worker and main cycle
	sendErrCh := make(chan error, 1)
	go func() { sendErrCh <- sender(sessCtx, stream, s) }()

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

	hbTimer := time.NewTimer(m.hbTTL)
	defer hbTimer.Stop()

	for {
		select {
		case in := <-inCh:
			resetTimer(hbTimer, m.hbTTL)

			switch x := in.Msg.(type) {
			case *controlpb.AgentToControl_Heartbeat:
				_ = m.svc.Heartbeat(ctx, agent.ID, x.Heartbeat.GetUptimeSeconds())

			case *controlpb.AgentToControl_Ack:
				_ = m.svc.Ack(ctx, agent.ID, x.Ack.GetSeq())
				s.lastAck.Store(x.Ack.GetSeq())

			case *controlpb.AgentToControl_Nack:
				_ = m.svc.Nack(ctx, agent.ID, x.Nack.GetSeq(), x.Nack.GetError())

			case *controlpb.AgentToControl_AllStats:

			case *controlpb.AgentToControl_UserStats:

			}

		case recvErr := <-recvErrCh:
			return m.finishStream(s, sendErrCh, normalizeRecvErr(recvErr))

		case <-hbTimer.C:
			return m.finishStream(s, sendErrCh, status.Error(codes.DeadlineExceeded, "heartbeat timeout"))
		}

	}
}

func (m *Manager) finishStream(s *session, sendErrCh <-chan error, cause error) error {
	m.cleanupSession(s)

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

func (m *Manager) cleanupSession(s *session) {
	s.cancel()
	m.reg.remove(s.agentID)
	close(s.sendCh)
}

func (m *Manager) flushPending(ctx context.Context, s *session) {
	from := s.lastAck.Load()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		wires, maxSeq, err := m.svc.FlushPending(ctx, s.agentID, from, m.outboxScanLimit)
		if err != nil {
			return
		}
		if len(wires) == 0 {
			s.recovering.Store(false)
			return
		}
		for _, w := range wires {
			if trySend(s, w) {
				if t := w.GetTask(); t != nil {
					_ = m.svc.MarkTaskSent(ctx, s.agentID, t.Meta.Seq)
				}
			}
		}
		if maxSeq > from {
			from = maxSeq
		}
	}
}
