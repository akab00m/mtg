package events

import (
	"context"
	"math/rand"
	"runtime"
	"sync/atomic"

	"github.com/9seconds/mtg/v2/mtglib"
	"github.com/OneOfOne/xxhash"
)

// EventStream is a default implementation of the [mtglib.EventStream]
// interface.
//
// EventStream manages a set of goroutines, observers. Main
// responsibility of the event stream is to route an event to relevant
// observer based on some hash so each observer will have all events
// which belong to some stream id.
//
// Thus, EventStream can spawn many observers.
type EventStream struct {
	ctx       context.Context
	ctxCancel context.CancelFunc
	chans     []chan mtglib.Event

	// dropped считает количество потерянных событий при overflow.
	// Указатель — EventStream использует value receiver, atomic.Uint64 содержит noCopy.
	dropped *atomic.Uint64
}

// Send delivers event to observer non-blocking.
// При переполнении канала EventTraffic события отбрасываются (drop-on-overflow)
// для предотвращения блокировки relay goroutine.
// Важные события (Start, Finish, Connect, Security) всегда доставляются блокирующе.
func (e EventStream) Send(ctx context.Context, evt mtglib.Event) {
	var chanNo uint32

	if streamID := evt.StreamID(); streamID != "" {
		chanNo = xxhash.ChecksumString32(streamID)
	} else {
		chanNo = rand.Uint32()
	}

	ch := e.chans[int(chanNo)%len(e.chans)]

	// EventTraffic — высокочастотное событие (каждый Read/Write в relay).
	// При slow Prometheus consumer (GC pause, disk IO) буфер 64 заполняется
	// за ~2 секунды, после чего relay goroutine блокируется на Send().
	// Это замедляет передачу данных клиенту — недопустимо для proxy.
	//
	// Остальные события (Start, Finish, ConnectedToDC, ReplayAttack и т.д.)
	// редкие и критичные для Prometheus метрик — для них блокировка допустима.
	if _, isTraffic := evt.(mtglib.EventTraffic); isTraffic {
		select {
		case <-ctx.Done():
		case <-e.ctx.Done():
		case ch <- evt:
		default:
			// Буфер переполнен — отбрасываем traffic event.
			// Метрики traffic будут чуть менее точными, но relay не блокируется.
			e.dropped.Add(1)
		}

		return
	}

	// Для некритичного пути (Start, Finish и т.д.) — блокирующая доставка.
	select {
	case <-ctx.Done():
	case <-e.ctx.Done():
	case ch <- evt:
	}
}

// Dropped возвращает количество отброшенных событий с момента старта.
func (e EventStream) Dropped() uint64 {
	return e.dropped.Load()
}

// Shutdown stops an event stream pipeline.
func (e EventStream) Shutdown() {
	e.ctxCancel()
}

// NewEventStream builds a new default event stream.
//
// If you give an empty array of observers, then NoopObserver is going
// to be used. If you give many observers, then they will process a
// message concurrently.
func NewEventStream(observerFactories []ObserverFactory) EventStream {
	if len(observerFactories) == 0 {
		observerFactories = append(observerFactories, NewNoopObserver)
	}

	ctx, cancel := context.WithCancel(context.Background())
	rv := EventStream{
		ctx:       ctx,
		ctxCancel: cancel,
		chans:     make([]chan mtglib.Event, runtime.NumCPU()),
		dropped:   &atomic.Uint64{},
	}

	for i := 0; i < runtime.NumCPU(); i++ {
		// Буфер 64: предотвращает блокировку relay при медленной обработке метрик.
		// connTraffic.Send() вызывается на каждый Read/Write — при буфере 1
		// relay ждёт observer'а, замедляя передачу данных клиенту.
		rv.chans[i] = make(chan mtglib.Event, 64)

		if len(observerFactories) == 1 {
			go eventStreamProcessor(ctx, rv.chans[i], observerFactories[0]())
		} else {
			go eventStreamProcessor(ctx, rv.chans[i], newMultiObserver(observerFactories))
		}
	}

	return rv
}

func eventStreamProcessor(ctx context.Context, eventChan <-chan mtglib.Event, observer Observer) { //nolint: cyclop
	defer observer.Shutdown()

	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-eventChan:
			switch typedEvt := evt.(type) {
			case mtglib.EventTraffic:
				observer.EventTraffic(typedEvt)
			case mtglib.EventStart:
				observer.EventStart(typedEvt)
			case mtglib.EventFinish:
				observer.EventFinish(typedEvt)
			case mtglib.EventConnectedToDC:
				observer.EventConnectedToDC(typedEvt)
			case mtglib.EventDomainFronting:
				observer.EventDomainFronting(typedEvt)
			case mtglib.EventIPBlocklisted:
				observer.EventIPBlocklisted(typedEvt)
			case mtglib.EventConcurrencyLimited:
				observer.EventConcurrencyLimited(typedEvt)
			case mtglib.EventReplayAttack:
				observer.EventReplayAttack(typedEvt)
			case mtglib.EventIPListSize:
				observer.EventIPListSize(typedEvt)
			case mtglib.EventDNSCacheMetrics:
				observer.EventDNSCacheMetrics(typedEvt)
			case mtglib.EventPoolMetrics:
				observer.EventPoolMetrics(typedEvt)
			case mtglib.EventRateLimiterMetrics:
				observer.EventRateLimiterMetrics(typedEvt)
			case mtglib.EventIPListCacheFallback:
				observer.EventIPListCacheFallback(typedEvt)
			}
		}
	}
}
