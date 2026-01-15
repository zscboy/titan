package locator

import (
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type LimitHandler struct {
	sem chan struct{}

	ipLimit int
	ipCount sync.Map

	lastGlobalLog int64 // unix nano
	lastIPLog     int64

	h http.Handler
}

func NewLimitHandler(h http.Handler, maxConns int, maxPerIP int) http.Handler {
	return &LimitHandler{
		sem:     make(chan struct{}, maxConns),
		ipLimit: maxPerIP,
		h:       h,
	}
}

func (l *LimitHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case l.sem <- struct{}{}:
		defer func() { <-l.sem }()
	default:
		if shouldLog(&l.lastGlobalLog, time.Minute) {
			log.Warn("LimitHandler: global concurrency limit reached")
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("server busy"))
		return
	}

	if l.ipLimit > 0 {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		v, _ := l.ipCount.LoadOrStore(ip, new(int32))
		if atomic.AddInt32(v.(*int32), 1) > int32(l.ipLimit) {
			atomic.AddInt32(v.(*int32), -1)

			if shouldLog(&l.lastIPLog, time.Minute) {
				log.Warnf("LimitHandler: ip %s reached concurrency limit (%d)", ip, l.ipLimit)
			}

			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("too many requests from this IP"))
			return
		}

		defer atomic.AddInt32(v.(*int32), -1)
	}

	l.h.ServeHTTP(w, r)
}

func shouldLog(last *int64, interval time.Duration) bool {
	now := time.Now().UnixNano()
	prev := atomic.LoadInt64(last)
	if now-prev < interval.Nanoseconds() {
		return false
	}
	return atomic.CompareAndSwapInt64(last, prev, now)
}
