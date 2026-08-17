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

type TextMessage struct {
	Channel   string
	Speaker   string
	MessageID uint32
	Text      string
	At        time.Time
}

type AzureSession interface {
	ID() string
	AskAudio(ctx context.Context, pcm []int16, sampleRate int, interim ToolInterims) ([]int16, int, error)
	AskText(ctx context.Context, text string) (string, error)
	Close(ctx context.Context) error
}

type AudioInterim func(ctx context.Context, pcm []int16, sampleRate int) error
type TextInterim func(ctx context.Context, text string) error

type ToolInterims struct {
	Audio AudioInterim
	Text  TextInterim
}

type AzureFactory interface {
	NewSession(ctx context.Context) (AzureSession, error)
}

type Transmitter interface {
	TransmitPCM(ctx context.Context, pcm []int16, sampleRate int) error
	TransmitText(ctx context.Context, text string) error
}

type Manager struct {
	mu          sync.Mutex
	conv        Conversation
	timeout     time.Duration
	botUsername string
	wakeWord    string
	detector    wakeword.Detector
	resampler   audio.Resampler
	azure       AzureFactory
	tx          Transmitter
	logger      *slog.Logger
	metrics     *metrics.Metrics
	session     AzureSession
	now         func() time.Time
}

func NewManager(timeout time.Duration, botUsername, wakeWord string, detector wakeword.Detector, resampler audio.Resampler, azure AzureFactory, tx Transmitter, logger *slog.Logger, metrics *metrics.Metrics) *Manager {
	return &Manager{
		conv:        Conversation{State: StateIdle},
		timeout:     timeout,
		botUsername: botUsername,
		wakeWord:    wakeWord,
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
	if state == StateActive && !m.conv.Busy && m.now().Sub(m.conv.LastActivity) > m.timeout {
		m.expireLocked(ctx, "timeout")
		state = StateIdle
	}
	m.mu.Unlock()

	var sendPCM []int16
	sendRate := t.SampleRate
	if len(t.PCM) == 0 {
		m.logger.Info("zello.rx_ignored", "speaker", t.Speaker, "reason", "empty_audio")
		return nil
	}
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
			m.logger.Debug("wakeword.transcript", "speaker", t.Speaker, "transcript", result.Transcript)
			return nil
		}
		m.metrics.WakeDetectionsTotal.Add(1)
		m.logger.Info("wakeword.detected", "speaker", t.Speaker, "query", result.Query)
		sendPCM = t.PCM
	} else {
		sendPCM = t.PCM
	}

	session, err := m.ensureSession(ctx, t.Speaker)
	if err != nil {
		return err
	}

	ctx = WithSpeaker(ctx, t.Speaker)

	m.mu.Lock()
	m.conv.Busy = true
	m.mu.Unlock()

	respPCM, respRate, err := session.AskAudio(ctx, sendPCM, sendRate, ToolInterims{
		Audio: m.tx.TransmitPCM,
		Text:  m.tx.TransmitText,
	})

	m.mu.Lock()
	m.conv.Busy = false
	m.mu.Unlock()

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

func (m *Manager) HandleTextMessage(ctx context.Context, msg TextMessage) error {
	if msg.Speaker == "" || msg.Speaker == m.botUsername {
		m.logger.Info("zello.text_ignored", "speaker", msg.Speaker, "reason", "self_or_unknown")
		return nil
	}

	m.mu.Lock()
	state := m.conv.State
	if state == StateActive && !m.conv.Busy && m.now().Sub(m.conv.LastActivity) > m.timeout {
		m.expireLocked(ctx, "timeout")
		state = StateIdle
	}
	m.mu.Unlock()

	text := msg.Text
	if state == StateIdle {
		result := wakeword.MatchTranscript(m.wakeWord, msg.Text)
		if !result.Activated {
			m.logger.Info("wakeword.not_detected", "speaker", msg.Speaker, "input", "text")
			return nil
		}
		m.metrics.WakeDetectionsTotal.Add(1)
		if result.Query != "" {
			text = result.Query
		}
		m.logger.Info("wakeword.detected", "speaker", msg.Speaker, "input", "text", "query", text)
	}

	session, err := m.ensureSession(ctx, msg.Speaker)
	if err != nil {
		return err
	}

	ctx = WithSpeaker(ctx, msg.Speaker)

	m.mu.Lock()
	m.conv.Busy = true
	m.mu.Unlock()

	reply, err := session.AskText(ctx, text)

	m.mu.Lock()
	m.conv.Busy = false
	m.mu.Unlock()

	if err != nil {
		m.End(ctx, "azure_error")
		return err
	}
	if reply == "" {
		return nil
	}
	if err := m.tx.TransmitText(ctx, reply); err != nil {
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
			if m.conv.State == StateActive && !m.conv.Busy && m.now().Sub(m.conv.LastActivity) > m.timeout {
				m.expireLocked(ctx, "timeout")
			}
			m.mu.Unlock()
		}
	}
}

func (m *Manager) ensureSession(ctx context.Context, speaker string) (AzureSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conv.State == StateIdle {
		m.conv.State = StateActivating
		m.conv.Speaker = speaker
		m.conv.LastActivity = m.now()
		m.logger.Info("conversation.started", "speaker", speaker)
	}
	if m.session == nil {
		session, err := m.azure.NewSession(ctx)
		if err != nil {
			m.conv.State = StateIdle
			return nil, err
		}
		m.session = session
		m.conv.SessionID = session.ID()
		m.conv.State = StateActive
		m.metrics.AzureSessionsTotal.Add(1)
		m.metrics.ConversationActive.Store(1)
		m.logger.Info("conversation.active", "session_id", session.ID())
	}
	m.conv.LastActivity = m.now()
	return m.session, nil
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
