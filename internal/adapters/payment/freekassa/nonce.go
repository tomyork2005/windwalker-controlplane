package freekassa

import (
	"strconv"
	"sync/atomic"
	"time"
)

type Noncer interface {
	Next() string // десятичная строка
}

type LocalAtomicNoncer struct{ last atomic.Int64 }

func (n *LocalAtomicNoncer) Next() string {
	for {
		now := time.Now().UnixNano()
		prev := n.last.Load()
		val := now
		if now <= prev {
			val = prev + 1
		}
		if n.last.CompareAndSwap(prev, val) {
			return strconv.FormatInt(val, 10)
		}
	}
}
