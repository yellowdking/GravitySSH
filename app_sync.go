package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/syncclient"
)

const (
	syncPreviewTTL         = 10 * time.Minute
	maxPendingSyncSessions = 8
	syncRelayCallTimeout   = 15 * time.Second
)

type syncPreviewEntry struct {
	bundle    models.SyncBundle
	expiresAt time.Time
}

type pendingSyncSendSession struct {
	sessionID     string
	channelID     string
	senderToken   string
	sendCode      string
	relayURL      string
	bundle        models.SyncBundle
	hasBundle     bool
	expiresAt     time.Time
	approved      bool
	approvedReqID string
}

type pendingSyncReceiveSession struct {
	waitID    string
	channelID string
	sendCode  string
	recvCode  string
	requestID string
	relayURL  string
	expiresAt time.Time
}

func zeroSendSession(s *pendingSyncSendSession) {
	if s == nil {
		return
	}
	clear(s.bundle.Servers)
	clear(s.bundle.SSHKeys)
	clear(s.bundle.Shortcuts)
	clear(s.bundle.Templates)
	s.bundle = models.SyncBundle{}
	s.hasBundle = false
	s.senderToken = ""
	s.sendCode = ""
}

type syncPreviewCache struct {
	mu              sync.Mutex
	entries         map[string]syncPreviewEntry
	pendingSends    map[string]*pendingSyncSendSession
	pendingReceives map[string]*pendingSyncReceiveSession
	timer           *time.Timer
	closed          bool
}

func newSyncPreviewCache() *syncPreviewCache {
	return &syncPreviewCache{
		entries:         make(map[string]syncPreviewEntry),
		pendingSends:    make(map[string]*pendingSyncSendSession),
		pendingReceives: make(map[string]*pendingSyncReceiveSession),
	}
}

func (c *syncPreviewCache) put(bundle models.SyncBundle) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ""
	}
	now := time.Now()
	c.pruneLocked(now)
	id := uuid.NewString()
	c.entries[id] = syncPreviewEntry{bundle: bundle, expiresAt: now.Add(syncPreviewTTL)}
	c.rescheduleLocked(now)
	return id
}

func (c *syncPreviewCache) pop(id string) (models.SyncBundle, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.pruneLocked(now)
	entry, ok := c.entries[id]
	if !ok {
		c.rescheduleLocked(now)
		return models.SyncBundle{}, false
	}
	delete(c.entries, id)
	c.rescheduleLocked(now)
	return entry.bundle, true
}

func (c *syncPreviewCache) putSend(sess *pendingSyncSendSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		zeroSendSession(sess)
		return
	}
	now := time.Now()
	c.pruneLocked(now)
	for len(c.pendingSends) >= maxPendingSyncSessions {
		var oldestKey string
		var oldestExp time.Time
		for k, v := range c.pendingSends {
			if oldestKey == "" || v.expiresAt.Before(oldestExp) {
				oldestKey = k
				oldestExp = v.expiresAt
			}
		}
		if oldestKey == "" {
			break
		}
		zeroSendSession(c.pendingSends[oldestKey])
		delete(c.pendingSends, oldestKey)
	}
	c.pendingSends[sess.sessionID] = sess
	c.rescheduleLocked(now)
}

func (c *syncPreviewCache) getSend(sessionID string) (*pendingSyncSendSession, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.pruneLocked(now)
	sess, ok := c.pendingSends[sessionID]
	if !ok {
		c.rescheduleLocked(now)
		return nil, false
	}
	cp := *sess
	c.rescheduleLocked(now)
	return &cp, true
}

func (c *syncPreviewCache) markSendApproved(sessionID, requestID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.pruneLocked(now)
	sess, ok := c.pendingSends[sessionID]
	if !ok || !sess.hasBundle || sess.approved {
		c.rescheduleLocked(now)
		return false
	}
	sess.approved = true
	sess.approvedReqID = requestID
	// Drop cached plaintext vault bundle once encrypted & uploaded to relay.
	clear(sess.bundle.Servers)
	clear(sess.bundle.SSHKeys)
	clear(sess.bundle.Shortcuts)
	clear(sess.bundle.Templates)
	sess.bundle = models.SyncBundle{}
	sess.hasBundle = false
	c.rescheduleLocked(now)
	return true
}

func (c *syncPreviewCache) popSend(sessionIDOrCode string) (*pendingSyncSendSession, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.pruneLocked(now)
	if sess, ok := c.pendingSends[sessionIDOrCode]; ok {
		delete(c.pendingSends, sessionIDOrCode)
		cp := *sess
		zeroSendSession(sess)
		c.rescheduleLocked(now)
		return &cp, true
	}
	if canonical, err := syncclient.CanonicalizeCode(sessionIDOrCode); err == nil {
		for k, sess := range c.pendingSends {
			if sess.sendCode == canonical {
				delete(c.pendingSends, k)
				cp := *sess
				zeroSendSession(sess)
				c.rescheduleLocked(now)
				return &cp, true
			}
		}
	}
	c.rescheduleLocked(now)
	return nil, false
}

func (c *syncPreviewCache) putReceive(sess *pendingSyncReceiveSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	now := time.Now()
	c.pruneLocked(now)
	for len(c.pendingReceives) >= maxPendingSyncSessions {
		var oldestKey string
		var oldestExp time.Time
		for k, v := range c.pendingReceives {
			if oldestKey == "" || v.expiresAt.Before(oldestExp) {
				oldestKey = k
				oldestExp = v.expiresAt
			}
		}
		if oldestKey == "" {
			break
		}
		delete(c.pendingReceives, oldestKey)
	}
	c.pendingReceives[sess.waitID] = sess
	c.rescheduleLocked(now)
}

func (c *syncPreviewCache) getReceive(waitID string) (*pendingSyncReceiveSession, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.pruneLocked(now)
	sess, ok := c.pendingReceives[waitID]
	if !ok {
		c.rescheduleLocked(now)
		return nil, false
	}
	cp := *sess
	c.rescheduleLocked(now)
	return &cp, true
}

func (c *syncPreviewCache) removeReceive(waitID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pendingReceives, waitID)
	c.rescheduleLocked(time.Now())
}

func (c *syncPreviewCache) clear(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if strings.TrimSpace(id) == "" {
		clear(c.entries)
		for k, s := range c.pendingSends {
			zeroSendSession(s)
			delete(c.pendingSends, k)
		}
		clear(c.pendingReceives)
	} else {
		delete(c.entries, id)
	}
	c.rescheduleLocked(time.Now())
}

func (c *syncPreviewCache) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	for k, s := range c.pendingSends {
		zeroSendSession(s)
		delete(c.pendingSends, k)
	}
	clear(c.pendingReceives)
	clear(c.entries)
}

func (c *syncPreviewCache) pruneLocked(now time.Time) {
	for k, v := range c.entries {
		if !now.Before(v.expiresAt) {
			delete(c.entries, k)
		}
	}
	for k, s := range c.pendingSends {
		if !now.Before(s.expiresAt) {
			zeroSendSession(s)
			delete(c.pendingSends, k)
		}
	}
	for k, r := range c.pendingReceives {
		if !now.Before(r.expiresAt) {
			delete(c.pendingReceives, k)
		}
	}
}

func (c *syncPreviewCache) rescheduleLocked(now time.Time) {
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	if c.closed || (len(c.entries) == 0 && len(c.pendingSends) == 0 && len(c.pendingReceives) == 0) {
		return
	}
	var earliest time.Time
	consider := func(t time.Time) {
		if earliest.IsZero() || t.Before(earliest) {
			earliest = t
		}
	}
	for _, v := range c.entries {
		consider(v.expiresAt)
	}
	for _, s := range c.pendingSends {
		consider(s.expiresAt)
	}
	for _, r := range c.pendingReceives {
		consider(r.expiresAt)
	}
	delay := time.Until(earliest)
	if delay < time.Millisecond {
		delay = time.Millisecond
	}
	c.timer = time.AfterFunc(delay, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		now := time.Now()
		c.pruneLocked(now)
		c.rescheduleLocked(now)
	})
}

func (a *App) syncContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) syncRelayContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(a.syncContext(), syncRelayCallTimeout)
}

func (a *App) resolveRelayURL(relayURL string) string {
	if trimmed := strings.TrimSpace(relayURL); trimmed != "" {
		return trimmed
	}
	if a.store != nil {
		if saved := strings.TrimSpace(a.store.GetSettings().SyncRelayURL); saved != "" {
			return saved
		}
	}
	return syncclient.DefaultRelayURL
}

func (a *App) GetDefaultSyncRelayURL() string { return syncclient.DefaultRelayURL }

// ClearSyncPreview discards a pending decrypted sync preview from memory
// (or all pending previews and pairing sessions when previewID is empty).
func (a *App) ClearSyncPreview(previewID string) {
	if a.syncCache != nil {
		a.syncCache.clear(previewID)
	}
}

// TestSyncRelay checks TLS 1.3 + App Gate connectivity to the given or saved relay URL.
func (a *App) TestSyncRelay(relayURL string) (int64, error) {
	if err := a.ensureReady(); err != nil {
		return 0, err
	}
	client, err := syncclient.NewClient(a.resolveRelayURL(relayURL))
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(a.syncContext(), 10*time.Second)
	defer cancel()
	return client.Ping(ctx)
}

func (a *App) StartSyncSend(relayURL string) (models.SyncSendSession, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.SyncSendSession{}, err
	}
	if a.syncCache == nil {
		return models.SyncSendSession{}, errors.New("同步缓存未初始化")
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "Windows-PC"
	}
	bundle, err := a.store.ExportSyncBundle(host)
	if err != nil {
		return models.SyncSendSession{}, err
	}
	code, err := syncclient.GenerateCode()
	if err != nil {
		return models.SyncSendSession{}, err
	}
	canonicalSend, err := syncclient.CanonicalizeCode(code)
	if err != nil {
		return models.SyncSendSession{}, err
	}
	qrText, err := syncclient.FormatQRText(code)
	if err != nil {
		return models.SyncSendSession{}, err
	}
	senderToken, err := syncclient.GenerateHexToken32()
	if err != nil {
		return models.SyncSendSession{}, err
	}
	channelID := syncclient.DeriveChannelID(canonicalSend)
	cleanURL, mode, err := syncclient.NormalizeRelayURL(a.resolveRelayURL(relayURL))
	if err != nil {
		return models.SyncSendSession{}, err
	}
	client, err := syncclient.NewClientWithMode(cleanURL, syncclient.AppGateKey, syncclient.PinnedSPKISHA256, mode)
	if err != nil {
		return models.SyncSendSession{}, err
	}
	ctx, cancel := a.syncRelayContext()
	defer cancel()
	expiresAt, err := client.PairInit(ctx, channelID, senderToken, syncclient.DefaultTTLSeconds)
	if err != nil {
		return models.SyncSendSession{}, err
	}
	sessionID := uuid.NewString()
	a.syncCache.putSend(&pendingSyncSendSession{
		sessionID:   sessionID,
		channelID:   channelID,
		senderToken: senderToken,
		sendCode:    canonicalSend,
		relayURL:    cleanURL,
		bundle:      bundle,
		hasBundle:   true,
		expiresAt:   expiresAt,
	})
	return models.SyncSendSession{
		SessionID:     sessionID,
		Code:          code,
		QRText:        qrText,
		ExpiresAt:     expiresAt.UTC().Format(time.RFC3339),
		RelayURL:      cleanURL,
		ServerCount:   len(bundle.Servers),
		KeyCount:      len(bundle.SSHKeys),
		ShortcutCount: len(bundle.Shortcuts),
		TemplateCount: len(bundle.Templates),
	}, nil
}

func (a *App) PollSyncSend(sessionID string) (models.SyncSendPollResult, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.SyncSendPollResult{}, err
	}
	if a.syncCache == nil {
		return models.SyncSendPollResult{}, errors.New("同步缓存未初始化")
	}
	sess, ok := a.syncCache.getSend(sessionID)
	if !ok {
		return models.SyncSendPollResult{Status: "expired", Message: "同步会话已过期或已撤销"}, nil
	}
	client, err := syncclient.NewClient(sess.relayURL)
	if err != nil {
		return models.SyncSendPollResult{}, err
	}
	ctx, cancel := a.syncRelayContext()
	defer cancel()
	res, err := client.PairPoll(ctx, sess.channelID, sess.senderToken)
	if err != nil {
		return models.SyncSendPollResult{}, err
	}
	if res.NotFound || res.State == "gone" {
		a.syncCache.popSend(sessionID)
		if sess.approved {
			return models.SyncSendPollResult{
				Status:    "consumed",
				RequestID: sess.approvedReqID,
				Message:   "对方已领取并销毁中继数据",
			}, nil
		}
		return models.SyncSendPollResult{
			Status:  "expired",
			Message: "同步码已过期或已被中继清理",
		}, nil
	}
	switch res.State {
	case "waiting":
		return models.SyncSendPollResult{Status: "waiting"}, nil
	case "requested":
		info, decErr := syncclient.DecryptPairRequest(sess.sendCode, res.EncryptedPayload)
		if decErr != nil {
			_ = client.PairReject(ctx, sess.channelID, sess.senderToken, res.RequestID)
			a.syncCache.popSend(sessionID)
			return models.SyncSendPollResult{
				Status:    "error",
				RequestID: res.RequestID,
				Message:   "接收端验证请求解密失败，已自动拒绝此次配对",
			}, nil
		}
		canonicalRecv, _ := syncclient.CanonicalizeCode(info.ReceiverCode)
		return models.SyncSendPollResult{
			Status:           "requested",
			RequestID:        res.RequestID,
			ReceiverCode:     syncclient.FormatDisplayCode(canonicalRecv),
			ReceiverDevice:   info.ReceiverDevice,
			ReceiverPlatform: info.ReceiverPlatform,
			RequestedAt:      info.RequestedAt,
		}, nil
	case "approved":
		return models.SyncSendPollResult{
			Status:    "approved",
			RequestID: sess.approvedReqID,
		}, nil
	case "rejected":
		a.syncCache.popSend(sessionID)
		return models.SyncSendPollResult{
			Status:    "rejected",
			RequestID: res.RequestID,
			Message:   "已拒绝该接收端请求",
		}, nil
	default:
		return models.SyncSendPollResult{Status: res.State, RequestID: res.RequestID}, nil
	}
}

func (a *App) ApproveSyncSend(sessionID, requestID, receiverCode string) error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	if a.syncCache == nil {
		return errors.New("同步缓存未初始化")
	}
	sess, ok := a.syncCache.getSend(sessionID)
	if !ok {
		return errors.New("同步会话已过期，请重新生成同步码")
	}
	if sess.approved || !sess.hasBundle {
		return errors.New("该同步会话已处理，请勿重复发送")
	}
	payload, err := syncclient.EncryptBundleDual(sess.sendCode, receiverCode, requestID, sess.bundle)
	if err != nil {
		return err
	}
	client, err := syncclient.NewClient(sess.relayURL)
	if err != nil {
		return err
	}
	ctx, cancel := a.syncRelayContext()
	defer cancel()
	if err := client.PairApprove(ctx, sess.channelID, sess.senderToken, requestID, payload); err != nil {
		return err
	}
	a.syncCache.markSendApproved(sessionID, requestID)
	return nil
}

func (a *App) RejectSyncSend(sessionID, requestID string) error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	if a.syncCache == nil {
		return nil
	}
	sess, ok := a.syncCache.popSend(sessionID)
	if !ok {
		return nil
	}
	client, err := syncclient.NewClient(sess.relayURL)
	if err != nil {
		return err
	}
	ctx, cancel := a.syncRelayContext()
	defer cancel()
	err = client.PairReject(ctx, sess.channelID, sess.senderToken, requestID)
	if errors.Is(err, syncclient.ErrPairNotFound) {
		return nil
	}
	return err
}

func (a *App) CancelSyncSend(sessionIDOrCode, relayURL string) error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	if a.syncCache == nil {
		return nil
	}
	sess, ok := a.syncCache.popSend(sessionIDOrCode)
	if !ok {
		return nil
	}
	targetURL := sess.relayURL
	if strings.TrimSpace(targetURL) == "" {
		targetURL = a.resolveRelayURL(relayURL)
	}
	client, err := syncclient.NewClient(targetURL)
	if err != nil {
		return err
	}
	ctx, cancel := a.syncRelayContext()
	defer cancel()
	err = client.PairCancel(ctx, sess.channelID, sess.senderToken)
	if errors.Is(err, syncclient.ErrPairNotFound) {
		return nil
	}
	return err
}

func (a *App) GenerateReceiverCode() (string, error) {
	if err := a.ensureUnlocked(); err != nil {
		return "", err
	}
	return syncclient.GenerateCode()
}

func (a *App) StartSyncReceive(sendCode, recvCode, relayURL string) (models.SyncReceiveWaitSession, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.SyncReceiveWaitSession{}, err
	}
	if a.syncCache == nil {
		return models.SyncReceiveWaitSession{}, errors.New("同步缓存未初始化")
	}
	canonicalSend, err := syncclient.CanonicalizeCode(sendCode)
	if err != nil {
		return models.SyncReceiveWaitSession{}, err
	}
	canonicalRecv, err := syncclient.CanonicalizeCode(recvCode)
	if err != nil {
		return models.SyncReceiveWaitSession{}, err
	}
	requestID, err := syncclient.GenerateHexToken32()
	if err != nil {
		return models.SyncReceiveWaitSession{}, err
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "Windows-PC"
	}
	displayRecv := syncclient.FormatDisplayCode(canonicalRecv)
	reqPayload, err := syncclient.EncryptPairRequest(canonicalSend, syncclient.PairRequestPayload{
		ReceiverCode:     displayRecv,
		ReceiverDevice:   host,
		ReceiverPlatform: "windows",
		RequestedAt:      time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return models.SyncReceiveWaitSession{}, err
	}
	cleanURL, mode, err := syncclient.NormalizeRelayURL(a.resolveRelayURL(relayURL))
	if err != nil {
		return models.SyncReceiveWaitSession{}, err
	}
	client, err := syncclient.NewClientWithMode(cleanURL, syncclient.AppGateKey, syncclient.PinnedSPKISHA256, mode)
	if err != nil {
		return models.SyncReceiveWaitSession{}, err
	}
	channelID := syncclient.DeriveChannelID(canonicalSend)
	ctx, cancel := a.syncRelayContext()
	defer cancel()
	expiresAt, err := client.PairRequest(ctx, channelID, requestID, reqPayload)
	if err != nil {
		return models.SyncReceiveWaitSession{}, err
	}
	waitID := uuid.NewString()
	a.syncCache.putReceive(&pendingSyncReceiveSession{
		waitID:    waitID,
		channelID: channelID,
		sendCode:  canonicalSend,
		recvCode:  canonicalRecv,
		requestID: requestID,
		relayURL:  cleanURL,
		expiresAt: expiresAt,
	})
	return models.SyncReceiveWaitSession{
		WaitID:       waitID,
		ReceiverCode: displayRecv,
		ExpiresAt:    expiresAt.UTC().Format(time.RFC3339),
	}, nil
}

func (a *App) PollSyncReceive(waitID string) (models.SyncReceivePollResult, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.SyncReceivePollResult{}, err
	}
	if a.syncCache == nil {
		return models.SyncReceivePollResult{}, errors.New("同步缓存未初始化")
	}
	sess, ok := a.syncCache.getReceive(waitID)
	if !ok {
		return models.SyncReceivePollResult{Status: "expired", Message: "配对等待已过期或已取消"}, nil
	}
	client, err := syncclient.NewClient(sess.relayURL)
	if err != nil {
		return models.SyncReceivePollResult{}, err
	}
	ctx, cancel := a.syncRelayContext()
	defer cancel()
	res, err := client.PairStatus(ctx, sess.channelID, sess.requestID)
	if errors.Is(err, syncclient.ErrPairNotFound) {
		a.syncCache.removeReceive(waitID)
		return models.SyncReceivePollResult{Status: "expired", Message: "同步码已过期或发送端已撤销"}, nil
	}
	if err != nil {
		return models.SyncReceivePollResult{}, err
	}
	switch res.State {
	case "pending":
		return models.SyncReceivePollResult{Status: "pending"}, nil
	case "rejected":
		a.syncCache.removeReceive(waitID)
		return models.SyncReceivePollResult{Status: "rejected", Message: "发送端已拒绝此次同步请求"}, nil
	case "approved":
		a.syncCache.removeReceive(waitID)
		bundle, err := syncclient.DecryptBundleDual(sess.sendCode, sess.recvCode, sess.requestID, res.EncryptedPayload)
		if err != nil {
			return models.SyncReceivePollResult{}, err
		}
		preview := models.SyncReceivePreview{
			PreviewID:      a.syncCache.put(bundle),
			SchemaVersion:  bundle.SchemaVersion,
			ExportedAt:     bundle.ExportedAt,
			SourcePlatform: bundle.SourcePlatform,
			SourceDevice:   bundle.SourceDevice,
			ServerCount:    len(bundle.Servers),
			KeyCount:       len(bundle.SSHKeys),
			ShortcutCount:  len(bundle.Shortcuts),
			TemplateCount:  len(bundle.Templates),
		}
		return models.SyncReceivePollResult{Status: "ready", Preview: &preview}, nil
	default:
		return models.SyncReceivePollResult{Status: res.State}, nil
	}
}

func (a *App) CancelSyncReceive(waitID string) {
	if a.syncCache != nil {
		a.syncCache.removeReceive(waitID)
	}
}

// PreviewSyncReceive retains gssh-sync-v1 single-code pickup compatibility.
func (a *App) PreviewSyncReceive(code, relayURL string) (models.SyncReceivePreview, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.SyncReceivePreview{}, err
	}
	if a.syncCache == nil {
		return models.SyncReceivePreview{}, errors.New("同步缓存未初始化")
	}
	canonical, err := syncclient.CanonicalizeCode(code)
	if err != nil {
		return models.SyncReceivePreview{}, err
	}
	client, err := syncclient.NewClient(a.resolveRelayURL(relayURL))
	if err != nil {
		return models.SyncReceivePreview{}, err
	}
	payload, err := client.Pickup(a.syncContext(), syncclient.DeriveChannelID(canonical))
	if err != nil {
		return models.SyncReceivePreview{}, err
	}
	bundle, err := syncclient.DecryptBundle(canonical, payload)
	if err != nil {
		return models.SyncReceivePreview{}, err
	}
	return models.SyncReceivePreview{
		PreviewID:      a.syncCache.put(bundle),
		SchemaVersion:  bundle.SchemaVersion,
		ExportedAt:     bundle.ExportedAt,
		SourcePlatform: bundle.SourcePlatform,
		SourceDevice:   bundle.SourceDevice,
		ServerCount:    len(bundle.Servers),
		KeyCount:       len(bundle.SSHKeys),
		ShortcutCount:  len(bundle.Shortcuts),
		TemplateCount:  len(bundle.Templates),
	}, nil
}

func (a *App) ApplySyncReceive(previewID, mode string) (models.SyncImportSummary, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.SyncImportSummary{}, err
	}
	if a.syncCache == nil {
		return models.SyncImportSummary{}, errors.New("同步缓存未初始化")
	}
	bundle, ok := a.syncCache.pop(previewID)
	if !ok {
		return models.SyncImportSummary{}, errors.New("预览已过期或已导入，请重新获取同步码")
	}
	return a.store.ImportSyncBundle(bundle, mode)
}
