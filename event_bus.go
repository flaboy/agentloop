package agentloop

import "sync"

type LoopEventBus struct {
	mu     sync.RWMutex
	nextID int
	subs   map[int]func(LoopEvent)
}

func NewLoopEventBus() *LoopEventBus {
	return &LoopEventBus{subs: map[int]func(LoopEvent){}}
}

func (b *LoopEventBus) Publish(event LoopEvent) {
	if b == nil || event == nil {
		return
	}
	b.mu.RLock()
	listeners := make([]func(LoopEvent), 0, len(b.subs))
	for _, fn := range b.subs {
		if fn != nil {
			listeners = append(listeners, fn)
		}
	}
	b.mu.RUnlock()
	for _, fn := range listeners {
		fn(event)
	}
}

func (b *LoopEventBus) Subscribe(fn func(LoopEvent)) func() {
	if b == nil || fn == nil {
		return func() {}
	}
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	b.subs[id] = fn
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		delete(b.subs, id)
		b.mu.Unlock()
	}
}
