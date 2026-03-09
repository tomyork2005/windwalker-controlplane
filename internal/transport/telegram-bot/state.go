package bot

import (
	"control-plane/internal/model"
	"sync"
)

type orderState struct {
	Region   string
	Protocol string
}

type SafeState struct {
	mu sync.RWMutex
	m  map[int64]orderState    // key - telebot.Context.ID(), value - user order state
	p  map[int64][]*model.Plan // actual plans, update at start new payment
}

func NewSafeState() *SafeState {
	return &SafeState{
		m: make(map[int64]orderState, 128),
		p: make(map[int64][]*model.Plan, 128),
	}
}

func (s *SafeState) Load(id int64) (orderState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[id]
	return v, ok
}

// Store перезаписывает состояние целиком.
func (s *SafeState) Store(id int64, value orderState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = value
}

func (s *SafeState) Delete(id int64) {
	s.mu.Lock()
	delete(s.m, id)
	delete(s.p, id)
	s.mu.Unlock()
}

func (s *SafeState) Update(id int64, fn func(cur orderState, ok bool) (next orderState, keep bool)) (orderState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.m[id]
	next, keep := fn(cur, ok)
	if !keep {
		delete(s.m, id)
		return orderState{}, false
	}
	s.m[id] = next
	return next, true
}

func (s *SafeState) GetActualPlans(id int64) ([]*model.Plan, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.p[id]
	return v, ok
}

func (s *SafeState) SetActualPlans(id int64, plans []*model.Plan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]*model.Plan, len(plans))
	copy(cp, plans)
	s.p[id] = cp
}
