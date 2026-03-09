package agent

import (
	"context"
	"control-plane/internal/model"
	"errors"
	"sync"
	"sync/atomic"
)

type session struct {
	agentID     string
	region      string
	driverTypes map[string]struct{}

	sendCh     chan model.Operation
	lastSeq    atomic.Uint64
	recovering atomic.Bool

	ctx    context.Context
	cancel context.CancelFunc
}

type registry struct {
	mu   sync.RWMutex
	byID map[string]*session
}

func newRegistry() *registry {
	return &registry{
		byID: make(map[string]*session),
	}
}

func (r *registry) addAgentOrSwap(s *session) *session {
	r.mu.Lock()
	defer r.mu.Unlock()

	old := r.byID[s.agentID]
	r.byID[s.agentID] = s
	return old
}

func (r *registry) remove(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byID, agentID)
}

func (r *registry) get(agentID string) (*session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.byID[agentID]
	return s, ok
}

func (r *registry) trySend(op model.Operation) error {
	s, ok := r.get(op.AgentID)
	if !ok {
		return errors.New("agent not found")
	}
	select {
	case s.sendCh <- op:
		return nil
	default:
		return errors.New("unknown error when trySend to agent")
	}
}

func toSet(ss []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		m[s] = struct{}{}
	}
	return m
}

func newSessionFromAgent(ctx context.Context, agent *model.Agent) *session {
	sessCtx, cancel := context.WithCancel(ctx)

	return &session{
		agentID:     agent.ID,
		region:      agent.Region,
		driverTypes: toSet(agent.DriverTypes),
		sendCh:      make(chan model.Operation, 1024),

		cancel: cancel,
		ctx:    sessCtx,
	}
}
