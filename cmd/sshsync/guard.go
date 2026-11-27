package main

import (
	"cmp"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	ratePerMin          = 10.0
	rateBurst           = 5.0
	pollRatePerMin      = 120.0
	pollBurst           = 20.0
	strikeLimit         = 10
	strikeDecayInterval = time.Minute
	banDuration         = 15 * time.Minute
	maxTrackedIPs       = 4096
	staleIPTTL          = 30 * time.Minute
	maxActiveConns      = 64
	maxConnsPerIP       = 8
	maxHeavyPerIP       = 1
)

type ipState struct {
	tokens        float64
	lastRefil     time.Time
	pollTokens    float64
	lastPollRefil time.Time
	lastSeen      time.Time
	strikes       int
	lastStrike    time.Time
	banUntil      time.Time
}

type ipGuard struct {
	mu          sync.Mutex
	ips         map[string]*ipState
	heavyActive map[string]int
	now         func() time.Time
	rateS       float64
	pollRateS   float64
}

func newIPGuard(nowFn func() time.Time) *ipGuard {
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &ipGuard{
		ips:         make(map[string]*ipState, 128),
		heavyActive: make(map[string]int, maxConcurrentApproves),
		now:         nowFn,
		rateS:       ratePerMin / 60.0,
		pollRateS:   pollRatePerMin / 60.0,
	}
}

func normalizeIPKey(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
		s = s[1 : len(s)-1]
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return s
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	addr = addr.WithZone("")
	prefix, err := addr.Prefix(64)
	if err != nil {
		return addr.String()
	}
	return prefix.String()
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
		tokens:        rateBurst,
		lastRefil:     now,
		pollTokens:    pollBurst,
		lastPollRefil: now,
		lastSeen:      now,
	}
	g.ips[ip] = st
	return st
}

func (g *ipGuard) sweepStaleLocked(now time.Time) {
	for k, st := range g.ips {
		unbanned := st.banUntil.IsZero() || !now.Before(st.banUntil)
		if unbanned && now.Sub(st.lastSeen) > staleIPTTL {
			delete(g.ips, k)
		}
	}
}

func (g *ipGuard) evictLocked(now time.Time) {
	// Pass 1: drop expired/stale unbanned entries.
	g.sweepStaleLocked(now)
	if len(g.ips) < maxTrackedIPs {
		return
	}

	// Pass 2: strictly enforce maxTrackedIPs in a ~10% batch to prevent OOM and O(N^2) churn.
	batch := max(1, maxTrackedIPs/10)
	target := maxTrackedIPs - batch

	type candidate struct {
		key      string
		primary  time.Time
		lastSeen time.Time
	}
	unbanned := make([]candidate, 0, len(g.ips))
	banned := make([]candidate, 0)
	for k, st := range g.ips {
		if st.banUntil.IsZero() || !now.Before(st.banUntil) {
			unbanned = append(unbanned, candidate{key: k, primary: st.lastSeen, lastSeen: st.lastSeen})
		} else {
			banned = append(banned, candidate{key: k, primary: st.banUntil, lastSeen: st.lastSeen})
		}
	}

	cmpCand := func(a, b candidate) int {
		if c := a.primary.Compare(b.primary); c != 0 {
			return c
		}
		if c := a.lastSeen.Compare(b.lastSeen); c != 0 {
			return c
		}
		return cmp.Compare(a.key, b.key)
	}

	if len(unbanned) > 0 {
		slices.SortFunc(unbanned, cmpCand)
		for _, c := range unbanned {
			if len(g.ips) <= target {
				return
			}
			delete(g.ips, c.key)
		}
	}

	if len(g.ips) > target && len(banned) > 0 {
		slices.SortFunc(banned, cmpCand)
		for _, c := range banned {
			if len(g.ips) <= target {
				return
			}
			delete(g.ips, c.key)
		}
	}
}

func (g *ipGuard) decayStrikesLocked(st *ipState, now time.Time) {
	if st.strikes <= 0 || st.lastStrike.IsZero() {
		return
	}
	elapsed := now.Sub(st.lastStrike)
	if elapsed < strikeDecayInterval {
		return
	}
	decayed := int(elapsed / strikeDecayInterval)
	if decayed >= st.strikes {
		st.strikes = 0
		st.lastStrike = time.Time{}
	} else {
		st.strikes -= decayed
		st.lastStrike = st.lastStrike.Add(time.Duration(decayed) * strikeDecayInterval)
	}
}

func (g *ipGuard) IsBanned(ip string) bool {
	ip = normalizeIPKey(ip)
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
		st.lastStrike = time.Time{}
	}
	return false
}

func consumeToken(tokens *float64, lastRefil *time.Time, now time.Time, rateS, burst float64) bool {
	elapsed := now.Sub(*lastRefil).Seconds()
	if elapsed > 0 {
		*tokens += elapsed * rateS
		if *tokens > burst {
			*tokens = burst
		}
		*lastRefil = now
	}
	if *tokens < 1.0 {
		return false
	}
	*tokens -= 1.0
	return true
}

func (g *ipGuard) AllowToken(ip string) bool {
	ip = normalizeIPKey(ip)
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now().UTC()
	st := g.getOrCreateLocked(ip, now)
	return consumeToken(&st.tokens, &st.lastRefil, now, g.rateS, rateBurst)
}

func (g *ipGuard) AllowPollToken(ip string) bool {
	ip = normalizeIPKey(ip)
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now().UTC()
	st := g.getOrCreateLocked(ip, now)
	return consumeToken(&st.pollTokens, &st.lastPollRefil, now, g.pollRateS, pollBurst)
}

func (g *ipGuard) RecordStrike(ip string) bool {
	ip = normalizeIPKey(ip)
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now().UTC()
	st := g.getOrCreateLocked(ip, now)
	if !st.banUntil.IsZero() {
		if now.Before(st.banUntil) {
			return true
		}
		st.banUntil = time.Time{}
		st.strikes = 0
		st.lastStrike = time.Time{}
	}
	g.decayStrikesLocked(st, now)
	st.strikes++
	st.lastStrike = now
	if st.strikes >= strikeLimit {
		st.banUntil = now.Add(banDuration)
		st.strikes = 0
		st.lastStrike = time.Time{}
		return true
	}
	return false
}

func (g *ipGuard) Strikes(ip string) int {
	ip = normalizeIPKey(ip)
	g.mu.Lock()
	defer g.mu.Unlock()

	st, ok := g.ips[ip]
	if !ok {
		return 0
	}
	now := g.now().UTC()
	if !st.banUntil.IsZero() {
		if now.Before(st.banUntil) {
			return 0
		}
		st.banUntil = time.Time{}
		st.strikes = 0
		st.lastStrike = time.Time{}
		return 0
	}
	g.decayStrikesLocked(st, now)
	return st.strikes
}

func (g *ipGuard) TryAcquireHeavy(ip string) bool {
	ip = normalizeIPKey(ip)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.heavyActive[ip] >= maxHeavyPerIP {
		return false
	}
	g.heavyActive[ip]++
	return true
}

func (g *ipGuard) ReleaseHeavy(ip string) {
	ip = normalizeIPKey(ip)
	g.mu.Lock()
	defer g.mu.Unlock()
	if n := g.heavyActive[ip]; n <= 1 {
		delete(g.heavyActive, ip)
	} else {
		g.heavyActive[ip] = n - 1
	}
}

func (g *ipGuard) Sweep() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweepStaleLocked(g.now().UTC())
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return normalizeIPKey(r.RemoteAddr)
	}
	return normalizeIPKey(host)
}

func remoteIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return normalizeIPKey(addr.String())
	}
	return normalizeIPKey(host)
}

type limitListener struct {
	net.Listener
	sem       chan struct{}
	closeCh   chan struct{}
	closeOnce sync.Once
	guard     *ipGuard

	mu       sync.Mutex
	perIP    map[string]int
	maxPerIP int
}

func newLimitListener(l net.Listener, maxConns, maxPerIP int, guard *ipGuard) net.Listener {
	if maxPerIP <= 0 {
		maxPerIP = maxConnsPerIP
	}
	return &limitListener{
		Listener: l,
		sem:      make(chan struct{}, maxConns),
		closeCh:  make(chan struct{}),
		guard:    guard,
		perIP:    make(map[string]int, maxConns),
		maxPerIP: maxPerIP,
	}
}

func (l *limitListener) Close() error {
	l.closeOnce.Do(func() { close(l.closeCh) })
	return l.Listener.Close()
}

func (l *limitListener) acquireIP(ip string) bool {
	if l.maxPerIP <= 0 || ip == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.perIP[ip] >= l.maxPerIP {
		return false
	}
	l.perIP[ip]++
	return true
}

func (l *limitListener) releaseIP(ip string) {
	if l.maxPerIP <= 0 || ip == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if n := l.perIP[ip]; n <= 1 {
		delete(l.perIP, ip)
	} else {
		l.perIP[ip] = n - 1
	}
}

func (l *limitListener) Accept() (net.Conn, error) {
	for {
		select {
		case <-l.closeCh:
			return nil, net.ErrClosed
		default:
		}
		select {
		case <-l.closeCh:
			return nil, net.ErrClosed
		case l.sem <- struct{}{}:
		}

		c, err := l.Listener.Accept()
		if err != nil {
			<-l.sem
			return nil, err
		}

		ip := remoteIP(c.RemoteAddr())
		if l.guard != nil && ip != "" && l.guard.IsBanned(ip) {
			_ = c.Close()
			<-l.sem
			continue
		}
		if !l.acquireIP(ip) {
			_ = c.Close()
			<-l.sem
			continue
		}

		return &limitConn{
			Conn: c,
			onClose: func() {
				l.releaseIP(ip)
				<-l.sem
			},
		}, nil
	}
}

type limitConn struct {
	net.Conn
	onClose func()
	once    sync.Once
}

func (c *limitConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.onClose)
	return err
}
