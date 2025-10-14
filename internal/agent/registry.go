package agent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

type session struct {
	agentID     string
	region      string
	driverTypes map[string]struct{}

	sendCh     chan *Task
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

func (r *registry) addOrSwap(s *session) (old *session) {
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

func (r *registry) sendByAgentID(task Task) error {
	err := task.Validate()
	if err != nil {
		return errors.New("invalid task")
	}

	s, ok := r.get(task.AgentID)
	if !ok {
		return errors.New("agent not found")
	}

	s.sendCh <- &task

	return nil
}

func toSet(ss []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		m[s] = struct{}{}
	}
	return m
}
