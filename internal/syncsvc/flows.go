package syncsvc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/vault"
	"github.com/yellowdking/synctool/sdk/go/client"
	synccrypto "github.com/yellowdking/synctool/sdk/go/crypto"
	"github.com/yellowdking/synctool/sdk/go/envelope"
)

type offerReadyOutcome struct {
	info client.OfferReadyInfo
	err  error
}

type flowSession struct {
	mu           sync.Mutex
	sessionID    string
	status       models.SyncFlowStatus
	createdAt    time.Time
	cancel       context.CancelFunc
	sasInputCh   chan string
	sasConfirmCh chan bool
	offerOnce    sync.Once
	offerCh      chan offerReadyOutcome
}

func (f *flowSession) snapshot() models.SyncFlowStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := f.status
	if f.status.PairedDevice != nil {
		pd := *f.status.PairedDevice
		cp.PairedDevice = &pd
	}
	if f.status.Preview != nil {
		pv := *f.status.Preview
		cp.Preview = &pv
	}
	return cp
}

func isTerminalFlowState(st string) bool {
	switch st {
	case "ready_preview", "completed", "error", "cancelled":
		return true
	default:
		return false
	}
}

func (m *Manager) registerFlow(purpose, role string, mode int, cancel context.CancelFunc) *flowSession {
	id := uuid.NewString()
	sess := &flowSession{
		sessionID: id,
		status: models.SyncFlowStatus{
			SessionID: id,
			Purpose:   purpose,
			Role:      role,
			Mode:      mode,
			State:     "waiting_peer",
		},
		createdAt:    time.Now(),
		cancel:       cancel,
		sasInputCh:   make(chan string, 1),
		sasConfirmCh: make(chan bool, 1),
		offerCh:      make(chan offerReadyOutcome, 1),
	}

	m.flowMu.Lock()
	for len(m.flows) >= maxTrackedFlows {
		var oldestID string
		var oldestAt time.Time
		for k, v := range m.flows {
			v.mu.Lock()
			term := isTerminalFlowState(v.status.State)
			at := v.createdAt
			v.mu.Unlock()
			if term && (oldestID == "" || at.Before(oldestAt)) {
				oldestID = k
				oldestAt = at
			}
		}
		if oldestID == "" {
			for k, v := range m.flows {
				if oldestID == "" || v.createdAt.Before(oldestAt) {
					oldestID = k
					oldestAt = v.createdAt
				}
			}
		}
		if oldestID == "" {
			break
		}
		if victim := m.flows[oldestID]; victim != nil && victim.cancel != nil {
			victim.cancel()
		}
		delete(m.flows, oldestID)
	}
	m.flows[id] = sess
	m.flowMu.Unlock()
	return sess
}

func (m *Manager) getFlow(sessionID string) (*flowSession, bool) {
	m.flowMu.Lock()
	sess, ok := m.flows[strings.TrimSpace(sessionID)]
	m.flowMu.Unlock()
	return sess, ok
}

func (m *Manager) updateFlow(sess *flowSession, mut func(st *models.SyncFlowStatus) bool) {
	sess.mu.Lock()
	emit := mut(&sess.status)
	snap := sess.status
	if sess.status.PairedDevice != nil {
		pd := *sess.status.PairedDevice
		snap.PairedDevice = &pd
	}
	if sess.status.Preview != nil {
		pv := *sess.status.Preview
		snap.Preview = &pv
	}
	sess.mu.Unlock()

	if emit {
		m.emitEvent("sync:flow_status", snap)
	}
}

func (m *Manager) finishFlowWithError(sess *flowSession, flowCtx context.Context, err error) {
	sess.offerOnce.Do(func() {
		sess.offerCh <- offerReadyOutcome{err: err}
	})
	m.updateFlow(sess, func(st *models.SyncFlowStatus) bool {
		if st.State == "cancelled" {
			return false
		}
		if flowCtx.Err() != nil || envelope.IsCode(err, envelope.ErrCodeCancelled) {
			st.State = "cancelled"
			if st.Message == "" {
				st.Message = "同步操作已取消"
			}
			return true
		}
		st.State = "error"
		st.Message = FormatSyncError(err).Error()
		return true
	})
}

func (m *Manager) cancelAllFlows(reason string) {
	m.flowMu.Lock()
	list := make([]*flowSession, 0, len(m.flows))
	for _, s := range m.flows {
		list = append(list, s)
	}
	clear(m.flows)
	m.flowMu.Unlock()

	for _, s := range list {
		if s.cancel != nil {
			s.cancel()
		}
		m.updateFlow(s, func(st *models.SyncFlowStatus) bool {
			if isTerminalFlowState(st.State) {
				return false
			}
			st.State = "cancelled"
			st.Message = reason
			return true
		})
	}
}

func (m *Manager) makeInitiatorSASCallback(sess *flowSession, flowCtx context.Context) func() (string, error) {
	calls := 0
	return func() (string, error) {
		calls++
		rem := max(1, 4-calls)
		m.updateFlow(sess, func(st *models.SyncFlowStatus) bool {
			st.State = "awaiting_sas_input"
			st.SASAttemptsRemaining = rem
			if calls > 1 {
				st.Message = fmt.Sprintf("6 位安全核验码不匹配，请重新输入（剩余 %d 次机会）", rem)
			} else {
				st.Message = "请输入接收端屏幕上显示的 6 位安全核验码"
			}
			return true
		})
		select {
		case code := <-sess.sasInputCh:
			m.updateFlow(sess, func(st *models.SyncFlowStatus) bool {
				st.State = "transferring"
				st.Message = ""
				return true
			})
			return code, nil
		case <-flowCtx.Done():
			return "", flowCtx.Err()
		}
	}
}

func (m *Manager) makeResponderSASConfirmCallback(sess *flowSession, flowCtx context.Context) func(string) error {
	return func(sas6 string) error {
		m.updateFlow(sess, func(st *models.SyncFlowStatus) bool {
			st.Mode = synccrypto.ModeCommitSAS
			st.State = "awaiting_sas_confirm"
			st.SAS = sas6
			st.Message = "请与发送端核对 6 位安全码是否完全一致"
			return true
		})
		select {
		case approved := <-sess.sasConfirmCh:
			if !approved {
				return errors.New("已拒绝安全码核验")
			}
			m.updateFlow(sess, func(st *models.SyncFlowStatus) bool {
				st.State = "transferring"
				st.Message = ""
				return true
			})
			return nil
		case <-flowCtx.Done():
			return flowCtx.Err()
		}
	}
}

func (m *Manager) makeProgressCallback(sess *flowSession) func(envelope.TransferEvent) {
	return func(ev envelope.TransferEvent) {
		m.updateFlow(sess, func(st *models.SyncFlowStatus) bool {
			if isTerminalFlowState(st.State) {
				return false
			}
			changed := st.State != "transferring"
			st.State = "transferring"
			st.BytesDone = ev.BytesDone
			st.BytesTotal = ev.BytesTotal
			return changed || ev.BytesDone == ev.BytesTotal
		})
	}
}

func (m *Manager) makeOfferReadyCallback(sess *flowSession) func(client.OfferReadyInfo) {
	return func(info client.OfferReadyInfo) {
		expStr := formatUnixRFC3339(info.ExpiresAt)
		m.updateFlow(sess, func(st *models.SyncFlowStatus) bool {
			st.State = "waiting_peer"
			st.PairURI = info.PairURI
			st.RoomCode = info.DisplayCode
			st.ExpiresAt = expStr
			return true
		})
		sess.offerOnce.Do(func() {
			sess.offerCh <- offerReadyOutcome{info: info}
		})
	}
}

func (m *Manager) waitOfferReady(ctx context.Context, sess *flowSession) (client.OfferReadyInfo, error) {
	waitCtx, cancel := context.WithTimeout(ctx, defaultOfferWaitMax)
	defer cancel()
	select {
	case out := <-sess.offerCh:
		if out.err != nil {
			if sess.cancel != nil {
				sess.cancel()
			}
			return client.OfferReadyInfo{}, FormatSyncError(out.err)
		}
		return out.info, nil
	case <-waitCtx.Done():
		if sess.cancel != nil {
			sess.cancel()
		}
		return client.OfferReadyInfo{}, errors.New("创建中继同步房间超时，请检查网络连接")
	}
}

// SendToDevice executes Flow 2 paired send ("auto" | "spool" | "pipe").
func (m *Manager) SendToDevice(ctx context.Context, store *vault.Store, deviceID, deliveryMode string) (models.SyncDirectSendResult, error) {
	mode := strings.ToLower(strings.TrimSpace(deliveryMode))
	if mode == "" {
		mode = client.DeliveryAuto
	}
	if mode != client.DeliveryAuto && mode != client.DeliverySpool && mode != client.DeliveryPipe {
		return models.SyncDirectSendResult{}, fmt.Errorf("无效的投递模式: %s", deliveryMode)
	}

	cli, _, relayMode, err := m.getOrCreateClient(store)
	if err != nil {
		return models.SyncDirectSendResult{}, FormatSyncError(err)
	}
	label, err := effectiveLocalLabel(store.SyncKeyStore())
	if err != nil {
		return models.SyncDirectSendResult{}, FormatSyncError(err)
	}
	bundle, err := store.ExportSyncBundle(label)
	if err != nil {
		return models.SyncDirectSendResult{}, err
	}
	parcel, cleanupParcel, err := BuildVaultParcel(bundle)
	ZeroSyncBundle(&bundle)
	if err != nil {
		return models.SyncDirectSendResult{}, FormatSyncError(err)
	}

	targetID := strings.TrimSpace(deviceID)
	if mode != client.DeliveryPipe {
		defer cleanupParcel()
		sendCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		res, sendErr := cli.SendToDevice(sendCtx, targetID, parcel, client.SendOptions{
			DeliveryMode: mode,
		})
		if sendErr != nil {
			if envelope.SuggestPipeMode(sendErr) {
				return models.SyncDirectSendResult{
					Status:  "suggest_pipe",
					Message: FormatSyncError(sendErr).Error(),
				}, nil
			}
			return models.SyncDirectSendResult{}, FormatSyncError(sendErr)
		}
		return models.SyncDirectSendResult{
			Status:    "sent_spool",
			MsgID:     res.MsgID,
			ExpiresAt: formatUnixRFC3339(res.ExpiresAt),
			Message:   "已发送至对方离线收件箱",
		}, nil
	}

	// Mode A Direct pipe transfer ("pipe")
	flowCtx, cancelFlow := context.WithTimeout(ctx, time.Duration(defaultFlowTTLSec+30)*time.Second)
	sess := m.registerFlow("direct", "initiator", synccrypto.ModeDirect, cancelFlow)

	go func() {
		defer cleanupParcel()
		defer cancelFlow()
		_, sendErr := cli.SendToDevice(flowCtx, targetID, parcel, client.SendOptions{
			DeliveryMode: client.DeliveryPipe,
			TTLSec:       defaultFlowTTLSec,
			IncludeRelay: relayMode == "custom",
			OnOfferReady: m.makeOfferReadyCallback(sess),
			OnProgress:   m.makeProgressCallback(sess),
		})
		if sendErr != nil {
			m.finishFlowWithError(sess, flowCtx, sendErr)
			return
		}
		m.updateFlow(sess, func(st *models.SyncFlowStatus) bool {
			st.State = "completed"
			st.Message = "对方已接收并校验完成"
			return true
		})
	}()

	offer, err := m.waitOfferReady(ctx, sess)
	if err != nil {
		return models.SyncDirectSendResult{}, err
	}
	return models.SyncDirectSendResult{
		Status:    "pipe_offer_ready",
		SessionID: sess.sessionID,
		PairURI:   offer.PairURI,
		RoomCode:  offer.DisplayCode,
		ExpiresAt: formatUnixRFC3339(offer.ExpiresAt),
		Message:   "直传已就绪，请在对端扫码或输入房间码",
	}, nil
}

// StartDeviceBind starts Flow 1 pairing as Initiator (mode=1 QR OOB or mode=2 8-char+SAS).
func (m *Manager) StartDeviceBind(ctx context.Context, store *vault.Store, mode int, includeRelay bool) (models.SyncFlowSession, error) {
	if mode != synccrypto.ModeQROOB && mode != synccrypto.ModeCommitSAS {
		return models.SyncFlowSession{}, errors.New("配对模式无效（支持 1=扫码 或 2=房间码+安全码）")
	}
	cli, _, _, err := m.getOrCreateClient(store)
	if err != nil {
		return models.SyncFlowSession{}, FormatSyncError(err)
	}
	label, err := effectiveLocalLabel(store.SyncKeyStore())
	if err != nil {
		return models.SyncFlowSession{}, FormatSyncError(err)
	}

	flowCtx, cancelFlow := context.WithTimeout(ctx, time.Duration(defaultFlowTTLSec+30)*time.Second)
	sess := m.registerFlow("pair", "initiator", mode, cancelFlow)

	go func() {
		defer cancelFlow()
		card, bindErr := cli.StartDeviceBind(flowCtx, client.PairOptions{
			Mode:         mode,
			Label:        label,
			IncludeRelay: includeRelay,
			TTLSec:       defaultFlowTTLSec,
			OnOfferReady: m.makeOfferReadyCallback(sess),
			OnSASInput:   m.makeInitiatorSASCallback(sess, flowCtx),
		})
		if bindErr != nil {
			m.finishFlowWithError(sess, flowCtx, bindErr)
			return
		}
		pd := toSyncPairedDevice(*card)
		m.updateFlow(sess, func(st *models.SyncFlowStatus) bool {
			st.State = "completed"
			st.PairedDevice = &pd
			st.Message = "设备双向配对成功"
			return true
		})
	}()

	offer, err := m.waitOfferReady(ctx, sess)
	if err != nil {
		return models.SyncFlowSession{}, err
	}
	return models.SyncFlowSession{
		SessionID: sess.sessionID,
		Purpose:   "pair",
		Role:      "initiator",
		Mode:      mode,
		State:     "waiting_peer",
		PairURI:   offer.PairURI,
		RoomCode:  offer.DisplayCode,
		ExpiresAt: formatUnixRFC3339(offer.ExpiresAt),
	}, nil
}

func validateCodeOrPairURI(raw string) (string, *envelope.RelayProfile, bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil, false, errors.New("请输入配对链接或 8 位房间码")
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "synctool://") {
		if _, err := envelope.ParsePairURI(trimmed); err != nil {
			return "", nil, false, FormatSyncError(err)
		}
		embProf, hasRelay, err := client.ParseRelayFromPairURI(trimmed)
		if err != nil {
			return "", nil, false, FormatSyncError(err)
		}
		return trimmed, embProf, hasRelay, nil
	}
	if _, err := synccrypto.NormalizeRoomID(trimmed); err != nil {
		return "", nil, false, errors.New("无效的 8 位房间码格式，请检查后重试")
	}
	return trimmed, nil, false, nil
}

// AcceptDeviceBind accepts a Flow 1 pairing offer as Responder (supporting 1-scan custom relay import).
func (m *Manager) AcceptDeviceBind(ctx context.Context, store *vault.Store, codeOrURI string) (models.SyncFlowSession, error) {
	cleanInput, embeddedRelay, hasRelay, err := validateCodeOrPairURI(codeOrURI)
	if err != nil {
		return models.SyncFlowSession{}, err
	}
	ks := store.SyncKeyStore()
	label, err := effectiveLocalLabel(ks)
	if err != nil {
		return models.SyncFlowSession{}, FormatSyncError(err)
	}

	var (
		cli        *client.Client
		ephemTmp   string
		closeEphem bool
	)
	if hasRelay && embeddedRelay != nil {
		cli, ephemTmp, err = m.newSDKClient(store, embeddedRelay)
		if err != nil {
			return models.SyncFlowSession{}, FormatSyncError(err)
		}
		closeEphem = true
	} else {
		cli, _, _, err = m.getOrCreateClient(store)
		if err != nil {
			return models.SyncFlowSession{}, FormatSyncError(err)
		}
	}

	flowCtx, cancelFlow := context.WithTimeout(ctx, time.Duration(defaultFlowTTLSec+30)*time.Second)
	sess := m.registerFlow("pair", "responder", 0, cancelFlow)

	go func() {
		defer cancelFlow()
		if closeEphem {
			defer func() {
				cli.Close()
				if ephemTmp != "" {
					_ = os.RemoveAll(ephemTmp)
				}
			}()
		}
		card, bindErr := cli.AcceptDeviceBind(flowCtx, cleanInput, client.PairOptions{
			Label:        label,
			OnSASConfirm: m.makeResponderSASConfirmCallback(sess, flowCtx),
		})
		if bindErr != nil {
			m.finishFlowWithError(sess, flowCtx, bindErr)
			return
		}
		// Persist embedded custom relay only AFTER cryptographic handshake & card verification succeed!
		if hasRelay && embeddedRelay != nil {
			_ = ks.SetCustomRelay(embeddedRelay)
			m.resetActiveClient()
		}
		pd := toSyncPairedDevice(*card)
		m.updateFlow(sess, func(st *models.SyncFlowStatus) bool {
			st.State = "completed"
			st.PairedDevice = &pd
			st.Message = "设备双向配对成功"
			return true
		})
	}()

	return models.SyncFlowSession{
		SessionID: sess.sessionID,
		Purpose:   "pair",
		Role:      "responder",
		State:     "waiting_peer",
	}, nil
}

// StartOneShotSend starts a Flow 1 one-shot transfer over Mode A zero-disk streaming pipe.
func (m *Manager) StartOneShotSend(ctx context.Context, store *vault.Store, mode int, includeRelay bool) (models.SyncFlowSession, error) {
	if mode != synccrypto.ModeQROOB && mode != synccrypto.ModeCommitSAS {
		return models.SyncFlowSession{}, errors.New("发送模式无效（支持 1=扫码速传 或 2=房间码+安全码）")
	}
	cli, _, _, err := m.getOrCreateClient(store)
	if err != nil {
		return models.SyncFlowSession{}, FormatSyncError(err)
	}
	label, err := effectiveLocalLabel(store.SyncKeyStore())
	if err != nil {
		return models.SyncFlowSession{}, FormatSyncError(err)
	}
	bundle, err := store.ExportSyncBundle(label)
	if err != nil {
		return models.SyncFlowSession{}, err
	}
	serverCount := len(bundle.Servers)
	keyCount := len(bundle.SSHKeys)
	shortcutCount := len(bundle.Shortcuts)
	templateCount := len(bundle.Templates)

	parcel, cleanupParcel, err := BuildVaultParcel(bundle)
	ZeroSyncBundle(&bundle)
	if err != nil {
		return models.SyncFlowSession{}, FormatSyncError(err)
	}

	flowCtx, cancelFlow := context.WithTimeout(ctx, time.Duration(defaultFlowTTLSec+60)*time.Second)
	sess := m.registerFlow("oneshot", "initiator", mode, cancelFlow)

	go func() {
		defer cleanupParcel()
		defer cancelFlow()
		_, sendErr := cli.StartOneShotSend(flowCtx, parcel, client.PairOptions{
			Mode:         mode,
			Label:        label,
			IncludeRelay: includeRelay,
			TTLSec:       defaultFlowTTLSec,
			OnOfferReady: m.makeOfferReadyCallback(sess),
			OnSASInput:   m.makeInitiatorSASCallback(sess, flowCtx),
			OnProgress:   m.makeProgressCallback(sess),
		})
		if sendErr != nil {
			m.finishFlowWithError(sess, flowCtx, sendErr)
			return
		}
		m.updateFlow(sess, func(st *models.SyncFlowStatus) bool {
			st.State = "completed"
			st.Message = "对端已成功接收并完成解密校验"
			return true
		})
	}()

	offer, err := m.waitOfferReady(ctx, sess)
	if err != nil {
		return models.SyncFlowSession{}, err
	}
	return models.SyncFlowSession{
		SessionID:     sess.sessionID,
		Purpose:       "oneshot",
		Role:          "initiator",
		Mode:          mode,
		State:         "waiting_peer",
		PairURI:       offer.PairURI,
		RoomCode:      offer.DisplayCode,
		ExpiresAt:     formatUnixRFC3339(offer.ExpiresAt),
		ServerCount:   serverCount,
		KeyCount:      keyCount,
		ShortcutCount: shortcutCount,
		TemplateCount: templateCount,
	}, nil
}

// StartReceive receives from a Mode A room (Flow 1 oneshot m=1/2 or Flow 2 paired direct m=0)
// and stages the verified SyncBundle into RAM preview cache for user merge/overwrite confirmation.
func (m *Manager) StartReceive(ctx context.Context, store *vault.Store, codeOrURI string) (models.SyncFlowSession, error) {
	cleanInput, embeddedRelay, hasRelay, err := validateCodeOrPairURI(codeOrURI)
	if err != nil {
		return models.SyncFlowSession{}, err
	}

	var (
		cli        *client.Client
		ephemTmp   string
		closeEphem bool
	)
	if hasRelay && embeddedRelay != nil {
		cli, ephemTmp, err = m.newSDKClient(store, embeddedRelay)
		if err != nil {
			return models.SyncFlowSession{}, FormatSyncError(err)
		}
		closeEphem = true
	} else {
		cli, _, _, err = m.getOrCreateClient(store)
		if err != nil {
			return models.SyncFlowSession{}, FormatSyncError(err)
		}
	}

	flowCtx, cancelFlow := context.WithTimeout(ctx, time.Duration(defaultFlowTTLSec+60)*time.Second)
	sess := m.registerFlow("receive", "responder", 0, cancelFlow)

	go func() {
		defer cancelFlow()
		if closeEphem {
			defer func() {
				cli.Close()
				if ephemTmp != "" {
					_ = os.RemoveAll(ephemTmp)
				}
			}()
		}

		var stagedPreview *models.SyncReceivePreview
		_, recvErr := cli.ReceiveFromRoom(flowCtx, cleanInput, func(p envelope.ReceivedParcel) error {
			bundle, decErr := DecodeReceivedParcel(p)
			if decErr != nil {
				return decErr
			}
			if m.stageFn == nil {
				ZeroSyncBundle(&bundle)
				return errors.New("预览缓存尚未初始化")
			}
			pv := m.stageFn(bundle)
			if pv.PreviewID == "" {
				return errors.New("预览缓存已关闭")
			}
			stagedPreview = &pv
			return nil
		}, client.ReceiveOptions{
			AppID:        AppID,
			OnSASConfirm: m.makeResponderSASConfirmCallback(sess, flowCtx),
			OnProgress:   m.makeProgressCallback(sess),
		})
		if recvErr != nil {
			m.finishFlowWithError(sess, flowCtx, recvErr)
			return
		}
		m.updateFlow(sess, func(st *models.SyncFlowStatus) bool {
			st.State = "ready_preview"
			st.Preview = stagedPreview
			st.Message = "已安全接收并解密同步包，请选择导入方式"
			return true
		})
	}()

	return models.SyncFlowSession{
		SessionID: sess.sessionID,
		Purpose:   "receive",
		Role:      "responder",
		State:     "waiting_peer",
	}, nil
}

// GetFlowStatus returns a snapshot of the specified interactive sync flow session.
func (m *Manager) GetFlowStatus(sessionID string) (models.SyncFlowStatus, error) {
	sess, ok := m.getFlow(sessionID)
	if !ok {
		return models.SyncFlowStatus{}, errors.New("同步会话不存在或已过期")
	}
	return sess.snapshot(), nil
}

// SubmitSASInput feeds the 6-digit SAS entered by the Initiator into the waiting m=2 handshake.
func (m *Manager) SubmitSASInput(sessionID, sasCode string) error {
	sess, ok := m.getFlow(sessionID)
	if !ok {
		return errors.New("同步会话不存在或已过期")
	}
	st := sess.snapshot()
	if st.State != "awaiting_sas_input" {
		return errors.New("当前同步会话不在等待输入安全核验码状态")
	}
	clean := strings.TrimSpace(sasCode)
	if clean == "" {
		return errors.New("请输入 6 位安全核验码")
	}
	select {
	case sess.sasInputCh <- clean:
		return nil
	default:
		return errors.New("安全核验码正在提交处理中，请稍候")
	}
}

// ConfirmSAS feeds the Responder's mandatory SAS visual verification decision into OnSASConfirm.
func (m *Manager) ConfirmSAS(sessionID string, approved bool) error {
	sess, ok := m.getFlow(sessionID)
	if !ok {
		return errors.New("同步会话不存在或已过期")
	}
	st := sess.snapshot()
	if st.State != "awaiting_sas_confirm" {
		return errors.New("当前同步会话不在等待确认安全核验码状态")
	}
	select {
	case sess.sasConfirmCh <- approved:
		return nil
	default:
		return errors.New("安全核验确认已提交")
	}
}

// CancelFlow aborts an active interactive sync flow and closes/burns its relay room.
func (m *Manager) CancelFlow(sessionID string) error {
	sess, ok := m.getFlow(sessionID)
	if !ok {
		return nil
	}
	if sess.cancel != nil {
		sess.cancel()
	}
	m.updateFlow(sess, func(st *models.SyncFlowStatus) bool {
		if isTerminalFlowState(st.State) {
			return false
		}
		st.State = "cancelled"
		st.Message = "同步操作已取消"
		return true
	})
	return nil
}
