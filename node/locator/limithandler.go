package locator

import "net/http"

type LimitHandler struct {
	sem chan struct{}
	h   http.Handler
}

func NewLimitHandler(h http.Handler, max int) http.Handler {
	return &LimitHandler{
		sem: make(chan struct{}, max),
		h:   h,
	}
}

func (l *LimitHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case l.sem <- struct{}{}:
		defer func() { <-l.sem }()
		l.h.ServeHTTP(w, r)
	default:
		// 熔断：直接拒绝
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("server busy"))
	}
}
