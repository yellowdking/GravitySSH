package main

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	ratePerMin     = 10.0
	rateBurst      = 5.0
	strikeLimit    = 10
	banDuration    = 15 * time.Minute
	maxTrackedIPs  = 4096
	staleIPTTL     = 30 * time.Minute
	maxActiveConns = 64
)

type ipState struct {
	tokens    float64
	lastRefil time.Time
	lastSeen  time.Time
	strikes   int
	banUntil  time.Time
}

type ipGuard struct {
	mu    sync.Mutex
	ips   map[string]*ipState
	now   func() time.Time
	rateS float64
}

func newIPGuard(nowFn func() time.Time) *ipGuard {
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &ipGuard{
		ips:   make(map[string]*ipState, 128),
		now:   nowFn,
		rateS: ratePerMin / 60.0,
	}
}

func (g *ipGuard) getOrCreateLocked(ip string, now time.Time) *ipState {
	if st, ok := g.ips[ip]; ok {
		st.lastSeen = now
		return st
	}
	if len(g.ips) >= maxTrackedIPs {
		g.evictLocked(now)
	}
	st := &ipState{
		tokens:    rateBurst,
		lastRefil: now,
		lastSeen:  now,
	}
	g.ips[ip] = st
	return st
}

func (g *ipGuard) evictLocked(now time.Time) {
	var oldestKey string
	var oldestTime time.Time
	for k, st := range g.ips {
		if now.After(st.banUntil) && now.Sub(st.lastSeen) > staleIPTTL {
			delete(g.ips, k)
			continue
		}
		if now.After(st.banUntil) && (oldestKey == "" || st.lastSeen.Before(oldestTime)) {
			oldestKey = k
			oldestTime = st.lastSeen
		}
	}
	if len(g.ips) >= maxTrackedIPs && oldestKey != "" {
		delete(g.ips, oldestKey)
	}
}

func (g *ipGuard) IsBanned(ip string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	st, ok := g.ips[ip]
	if !ok {
		return false
	}
	now := g.now().UTC()
	if !st.banUntil.IsZero() {
		if now.Before(st.banUntil) {
			return true
		}
		st.banUntil = time.Time{}
		st.strikes = 0
	}
	return false
}

func (g *ipGuard) AllowToken(ip string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now().UTC()
	st := g.getOrCreateLocked(ip, now)
	elapsed := now.Sub(st.lastRefil).Seconds()
	if elapsed > 0 {
		st.tokens += elapsed * g.rateS
		if st.tokens > rateBurst {
			st.tokens = rateBurst
		}
		st.lastRefil = now
	}
	if st.tokens < 1.0 {
		return false
	}
	st.tokens -= 1.0
	return true
}

func (g *ipGuard) RecordStrike(ip string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now().UTC()
	st := g.getOrCreateLocked(ip, now)
	if !st.banUntil.IsZero() && !now.Before(st.banUntil) {
		st.banUntil = time.Time{}
		st.strikes = 0
	}
	st.strikes++
	if st.strikes >= strikeLimit {
		st.banUntil = now.Add(banDuration)
		st.strikes = 0
		return true
	}
	return false
}

func (g *ipGuard) Sweep() {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now().UTC()
	for k, st := range g.ips {
		if now.After(st.banUntil) && now.Sub(st.lastSeen) > staleIPTTL {
			delete(g.ips, k)
		}
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type limitListener struct {
	net.Listener
	sem chan struct{}
}

func newLimitListener(l net.Listener, max int) net.Listener {
	return &limitListener{
		Listener: l,
		sem:      make(chan struct{}, max),
	}
}

func (l *limitListener) Accept() (net.Conn, error) {
	l.sem <- struct{}{}
	c, err := l.Listener.Accept()
	if err != nil {
		<-l.sem
		return nil, err
	}
	return &limitConn{Conn: c, sem: l.sem}, nil
}

type limitConn struct {
	net.Conn
	sem  chan struct{}
	once sync.Once
}

func (c *limitConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { <-c.sem })
	return err
}
