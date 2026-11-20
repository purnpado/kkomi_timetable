package eventbus

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"kkomi-timetable/pkg/protocol"
)

// Handler는 도메인 이벤트를 처리하는 구독자 콜백 함수 타입입니다.
type Handler func(event protocol.DomainEvent)

// Bus는 모듈 간 결합도를 낮추기 위한 중앙 도메인 이벤트 버스(Pub/Sub)입니다.
type Bus struct {
	mu          sync.RWMutex
	seq         atomic.Uint64
	subscribers map[protocol.EventType][]Handler
	allHandlers []Handler
	history     []protocol.DomainEvent
}

// New는 새로운 중앙 도메인 이벤트 버스를 생성합니다.
func New() *Bus {
	return &Bus{
		subscribers: make(map[protocol.EventType][]Handler),
		history:     make([]protocol.DomainEvent, 0, 128),
	}
}

// Subscribe는 특정 이벤트 타입을 구독합니다.
func (b *Bus) Subscribe(eventType protocol.EventType, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[eventType] = append(b.subscribers[eventType], handler)
}

// SubscribeAll은 모든 도메인 이벤트를 수신하는 범용 구독자(예: WebSocket 허브, 감사 로그)를 등록합니다.
func (b *Bus) SubscribeAll(handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.allHandlers = append(b.allHandlers, handler)
}

// Publish는 이벤트를 기록하고 동기/비동기 구독자들에게 전파합니다.
// 모듈 간 연쇄 로직(예: 외부감독 확정 -> 결보강 대기열 등록)의 정합성을 위해 특정 핸들러는 즉시 호출하고,
// 브로드캐스트 핸들러는 고루틴으로 비동기 전파합니다.
func (b *Bus) Publish(event protocol.DomainEvent) protocol.DomainEvent {
	if event.ID == "" {
		event.ID = fmt.Sprintf("evt-%d-%d", time.Now().UnixMilli(), b.seq.Add(1))
	}
	if event.OccurredAt == "" {
		event.OccurredAt = time.Now().Format("2006-01-02 15:04:05")
	}

	b.mu.Lock()
	b.history = append(b.history, event)
	if len(b.history) > 300 {
		b.history = b.history[len(b.history)-300:]
	}
	typed := append([]Handler(nil), b.subscribers[event.Type]...)
	globals := append([]Handler(nil), b.allHandlers...)
	b.mu.Unlock()

	for _, h := range typed {
		safeInvoke(h, event)
	}
	for _, h := range globals {
		handler := h
		go safeInvoke(handler, event)
	}
	return event
}

func safeInvoke(h Handler, evt protocol.DomainEvent) {
	if h == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[EventBus] 이벤트(%s) 구독자 패닉 복구: %v", evt.Type, r)
		}
	}()
	h(evt)
}

// RecentEvents는 최근 발행된 이벤트 목록을 최신순으로 반환합니다.
func (b *Bus) RecentEvents(limit int) []protocol.DomainEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if limit <= 0 || limit > len(b.history) {
		limit = len(b.history)
	}
	result := make([]protocol.DomainEvent, 0, limit)
	for i := len(b.history) - 1; i >= len(b.history)-limit; i-- {
		result = append(result, b.history[i])
	}
	return result
}
