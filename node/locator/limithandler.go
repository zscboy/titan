package locator

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	ErrServerBusy = "server busy"
	ErrIPLimited  = "too many requests from this IP"
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
		if l.shouldLog(&l.lastGlobalLog, 30*time.Second) {
			log.Errorf("LimitHandler: global concurrency limit reached")
		}
		http.Error(w, ErrServerBusy, http.StatusServiceUnavailable)
		return
	}

	if l.ipLimit > 0 {
		remoteAddr := r.RemoteAddr
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			remoteAddr = strings.Split(xff, ",")[0]
		}

		ip, _, err := net.SplitHostPort(remoteAddr)
		if err != nil {
			ip = remoteAddr // Fallback if no port present
		}

		v, ok := l.ipCount.Load(ip)
		if !ok {
			v, _ = l.ipCount.LoadOrStore(ip, new(int32))
		}

		if atomic.AddInt32(v.(*int32), 1) > int32(l.ipLimit) {
			atomic.AddInt32(v.(*int32), -1)

			if l.shouldLog(&l.lastIPLog, 30*time.Second) {
				log.Errorf("LimitHandler: ip %s reached concurrency limit (%d)", ip, l.ipLimit)
			}

			http.Error(w, ErrIPLimited, http.StatusTooManyRequests)
			return
		}

		defer func() {
			if atomic.AddInt32(v.(*int32), -1) <= 0 {
				l.ipCount.Delete(ip)
			}
		}()
	}

	l.h.ServeHTTP(w, r)
}

func (l *LimitHandler) shouldLog(last *int64, interval time.Duration) bool {
	now := time.Now().UnixNano()
	prev := atomic.LoadInt64(last)
	if now-prev < interval.Nanoseconds() {
		return false
	}
	return atomic.CompareAndSwapInt64(last, prev, now)
}
