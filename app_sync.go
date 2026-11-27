package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/syncclient"
)

const syncRelayCallTimeout = 15 * time.Second

func (a *App) syncContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) syncRelayContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(a.syncContext(), syncRelayCallTimeout)
}

func resolveHostName() string {
	host, _ := os.Hostname()
	if strings.TrimSpace(host) == "" {
		return "Windows-PC"
	}
	return host
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

func (a *App) syncClient(relayURL string) (*syncclient.Client, string, error) {
	cleanURL, mode, err := syncclient.NormalizeRelayURL(a.resolveRelayURL(relayURL))
	if err != nil {
		return nil, "", err
	}
	a.syncClientMu.Lock()
	defer a.syncClientMu.Unlock()
	if a.syncClients == nil {
		a.syncClients = make(map[string]*syncclient.Client)
	}
	if cli, ok := a.syncClients[cleanURL]; ok {
		return cli, cleanURL, nil
	}
	cli, err := syncclient.NewClientWithMode(cleanURL, syncclient.AppGateKey, syncclient.PinnedSPKISHA256, mode)
	if err != nil {
		return nil, "", err
	}
	a.syncClients[cleanURL] = cli
	return cli, cleanURL, nil
}

func (a *App) buildSyncPreview(bundle models.SyncBundle) models.SyncReceivePreview {
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
	}
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
	client, _, err := a.syncClient(relayURL)
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
	bundle, err := a.store.ExportSyncBundle(resolveHostName())
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
	client, cleanURL, err := a.syncClient(relayURL)
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
	client, _, err := a.syncClient(sess.relayURL)
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
	client, _, err := a.syncClient(sess.relayURL)
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
	client, _, err := a.syncClient(sess.relayURL)
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
		targetURL = relayURL
	}
	client, _, err := a.syncClient(targetURL)
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
	displayRecv := syncclient.FormatDisplayCode(canonicalRecv)
	reqPayload, err := syncclient.EncryptPairRequest(canonicalSend, syncclient.PairRequestPayload{
		ReceiverCode:     displayRecv,
		ReceiverDevice:   resolveHostName(),
		ReceiverPlatform: "windows",
		RequestedAt:      time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return models.SyncReceiveWaitSession{}, err
	}
	client, cleanURL, err := a.syncClient(relayURL)
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
	client, _, err := a.syncClient(sess.relayURL)
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
		preview := a.buildSyncPreview(bundle)
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
