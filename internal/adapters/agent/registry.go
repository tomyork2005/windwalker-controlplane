package agent

import (
	"context"
	"sync"
	"sync/atomic"
)

type session struct {
	agentID     string
	region      string
	driverTypes map[string]struct{}

	sendCh     chan Operation
	lastAck    atomic.Uint64
	recovering atomic.Bool
	cancel     context.CancelFunc
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

func (r *registry) addAgentOrSwap(s *session) (old *session) {
	r.mu.Lock()
	defer r.mu.Unlock()

	old = r.byID[s.agentID]
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

func (r *registry) trySend(op Operation) bool {
	s, ok := r.get(op.AgentID)
	if !ok {
		return false
	}
	select {
	case s.sendCh <- op:
		return true
	default:
		return false
	}
}

func toSet(ss []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		m[s] = struct{}{}
	}
	return m
}
