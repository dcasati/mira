package conversation

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/dcasati/mira/internal/audio"
	"github.com/dcasati/mira/internal/metrics"
	"github.com/dcasati/mira/internal/wakeword"
)

type Transmission struct {
	Channel    string
	Speaker    string
	StreamID   uint32
	PCM        []int16
	SampleRate int
	StartedAt  time.Time
	EndedAt    time.Time
}

type AzureSession interface {
	ID() string
	AskAudio(ctx context.Context, pcm []int16, sampleRate int) ([]int16, int, error)
	Close(ctx context.Context) error
}

type AzureFactory interface {
	NewSession(ctx context.Context) (AzureSession, error)
}

type Transmitter interface {
	TransmitPCM(ctx context.Context, pcm []int16, sampleRate int) error
}

type Manager struct {
	mu          sync.Mutex
	conv        Conversation
	timeout     time.Duration
	botUsername string
	detector    wakeword.Detector
	resampler   audio.Resampler
	azure       AzureFactory
	tx          Transmitter
	logger      *slog.Logger
	metrics     *metrics.Metrics
	session     AzureSession
	now         func() time.Time
}

func NewManager(timeout time.Duration, botUsername string, detector wakeword.Detector, resampler audio.Resampler, azure AzureFactory, tx Transmitter, logger *slog.Logger, metrics *metrics.Metrics) *Manager {
	return &Manager{
		conv:        Conversation{State: StateIdle},
		timeout:     timeout,
		botUsername: botUsername,
		detector:    detector,
		resampler:   resampler,
		azure:       azure,
		tx:          tx,
		logger:      logger,
		metrics:     metrics,
		now:         time.Now,
	}
}

func (m *Manager) Snapshot() Conversation {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.conv
}

func (m *Manager) HandleTransmission(ctx context.Context, t Transmission) error {
	if t.Speaker == "" || t.Speaker == m.botUsername {
		m.logger.Info("zello.rx_ignored", "speaker", t.Speaker, "reason", "self_or_unknown")
		return nil
	}

	m.mu.Lock()
	state := m.conv.State
	if state == StateActive && m.now().Sub(m.conv.LastActivity) > m.timeout {
		m.expireLocked(ctx, "timeout")
		state = StateIdle
	}
	m.mu.Unlock()

	var sendPCM []int16
	sendRate := t.SampleRate
	if state == StateIdle {
		wakePCM, err := m.resampler.Resample(t.PCM, t.SampleRate, audio.WakeSampleRate)
		if err != nil {
			return err
		}
		result, err := m.detector.Detect(ctx, wakePCM, audio.WakeSampleRate)
		if err != nil {
			return err
		}
		if !result.Activated {
			m.logger.Info("wakeword.not_detected", "speaker", t.Speaker)
			return nil
		}
		m.metrics.WakeDetectionsTotal.Add(1)
		m.logger.Info("wakeword.detected", "speaker", t.Speaker, "query", result.Query)
		sendPCM = t.PCM
	} else {
		sendPCM = t.PCM
	}

	m.mu.Lock()
	if m.conv.State == StateIdle {
		m.conv.State = StateActivating
		m.conv.Speaker = t.Speaker
		m.conv.LastActivity = m.now()
		m.logger.Info("conversation.started", "speaker", t.Speaker)
	}
	session := m.session
	if session == nil {
		var err error
		session, err = m.azure.NewSession(ctx)
		if err != nil {
			m.conv.State = StateIdle
			m.mu.Unlock()
			return err
		}
		m.session = session
		m.conv.SessionID = session.ID()
		m.conv.State = StateActive
		m.metrics.AzureSessionsTotal.Add(1)
		m.metrics.ConversationActive.Store(1)
		m.logger.Info("conversation.active", "session_id", session.ID())
	}
	m.conv.LastActivity = m.now()
	m.mu.Unlock()

	respPCM, respRate, err := session.AskAudio(ctx, sendPCM, sendRate)
	if err != nil {
		m.End(ctx, "azure_error")
		return err
	}
	if len(respPCM) == 0 {
		return nil
	}
	if err := m.tx.TransmitPCM(ctx, respPCM, respRate); err != nil {
		return err
	}

	m.mu.Lock()
	if m.conv.State == StateActive {
		m.conv.LastActivity = m.now()
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) RunExpiryLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.End(context.Background(), "shutdown")
			return
		case <-ticker.C:
			m.mu.Lock()
			if m.conv.State == StateActive && m.now().Sub(m.conv.LastActivity) > m.timeout {
				m.expireLocked(ctx, "timeout")
			}
			m.mu.Unlock()
		}
	}
}

func (m *Manager) End(ctx context.Context, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked(ctx, reason)
}

func (m *Manager) expireLocked(ctx context.Context, reason string) {
	if m.session != nil {
		m.conv.State = StateExpiring
		m.logger.Info("conversation.expired", "session_id", m.session.ID(), "reason", reason)
		_ = m.session.Close(ctx)
	}
	m.session = nil
	m.conv = Conversation{State: StateIdle}
	m.metrics.ConversationActive.Store(0)
	m.logger.Info("conversation.ended", "reason", reason)
}
