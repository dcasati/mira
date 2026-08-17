package conversation

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/dcasati/mira/internal/audio"
	"github.com/dcasati/mira/internal/metrics"
	"github.com/dcasati/mira/internal/wakeword"
)

type fakeDetector struct{ result wakeword.Result }

func (d fakeDetector) Detect(context.Context, []int16, int) (wakeword.Result, error) {
	return d.result, nil
}
func (d fakeDetector) Ready() bool  { return true }
func (d fakeDetector) Close() error { return nil }

type fakeAzureFactory struct {
	sessions int
	session  *fakeSession
}

func (f *fakeAzureFactory) NewSession(context.Context) (AzureSession, error) {
	f.sessions++
	f.session = &fakeSession{id: "sess-test"}
	return f.session, nil
}

type fakeSession struct {
	id             string
	asks           int
	texts          int
	closed         bool
	lastSpeakerCtx string
}

func (s *fakeSession) ID() string { return s.id }
func (s *fakeSession) AskAudio(ctx context.Context, _ []int16, _ int, _ ToolInterims) ([]int16, int, error) {
	s.asks++
	s.lastSpeakerCtx = SpeakerFromContext(ctx)
	return []int16{1, 2, 3}, audio.AzureSampleRate, nil
}
func (s *fakeSession) AskText(ctx context.Context, _ string) (string, error) {
	s.texts++
	s.lastSpeakerCtx = SpeakerFromContext(ctx)
	return "text reply", nil
}
func (s *fakeSession) Close(context.Context) error {
	s.closed = true
	return nil
}

type fakeTX struct {
	count     int
	textCount int
}

func (t *fakeTX) TransmitPCM(context.Context, []int16, int) error {
	t.count++
	return nil
}
func (t *fakeTX) TransmitText(context.Context, string) error {
	t.textCount++
	return nil
}

func TestInactiveTransmissionIgnored(t *testing.T) {
	azure := &fakeAzureFactory{}
	tx := &fakeTX{}
	m := testManager(fakeDetector{result: wakeword.Result{}}, azure, tx)
	if err := m.HandleTransmission(context.Background(), testTransmission("alice")); err != nil {
		t.Fatal(err)
	}
	if azure.sessions != 0 || tx.count != 0 {
		t.Fatalf("unexpected sessions=%d tx=%d", azure.sessions, tx.count)
	}
}

func TestActivationAndFollowupUseSameSession(t *testing.T) {
	azure := &fakeAzureFactory{}
	tx := &fakeTX{}
	m := testManager(fakeDetector{result: wakeword.Result{Activated: true, Query: "tell me a joke"}}, azure, tx)
	if err := m.HandleTransmission(context.Background(), testTransmission("alice")); err != nil {
		t.Fatal(err)
	}
	if got := m.Snapshot().State; got != StateActive {
		t.Fatalf("state = %s", got)
	}
	if err := m.HandleTransmission(context.Background(), testTransmission("alice")); err != nil {
		t.Fatal(err)
	}
	if azure.sessions != 1 {
		t.Fatalf("sessions = %d", azure.sessions)
	}
	if azure.session.asks != 2 {
		t.Fatalf("asks = %d", azure.session.asks)
	}
	if azure.session.lastSpeakerCtx != "alice" {
		t.Fatalf("lastSpeakerCtx = %q, want %q (Manager must attach the speaker to ctx so Foundry session-reuse can key on it)", azure.session.lastSpeakerCtx, "alice")
	}
}

func TestConversationTimeoutReturnsIdle(t *testing.T) {
	azure := &fakeAzureFactory{}
	tx := &fakeTX{}
	m := testManager(fakeDetector{result: wakeword.Result{Activated: true}}, azure, tx)
	now := time.Now()
	m.now = func() time.Time { return now }
	if err := m.HandleTransmission(context.Background(), testTransmission("alice")); err != nil {
		t.Fatal(err)
	}
	now = now.Add(46 * time.Second)
	if err := m.HandleTransmission(context.Background(), testTransmission("alice")); err != nil {
		t.Fatal(err)
	}
	if azure.sessions != 2 {
		t.Fatalf("expected new session after timeout, got %d", azure.sessions)
	}
}

func TestSelfTransmissionIgnored(t *testing.T) {
	azure := &fakeAzureFactory{}
	tx := &fakeTX{}
	m := testManager(fakeDetector{result: wakeword.Result{Activated: true}}, azure, tx)
	if err := m.HandleTransmission(context.Background(), testTransmission("mira-bot")); err != nil {
		t.Fatal(err)
	}
	if azure.sessions != 0 {
		t.Fatalf("sessions = %d", azure.sessions)
	}
}

func TestGracefulConversationClose(t *testing.T) {
	azure := &fakeAzureFactory{}
	tx := &fakeTX{}
	m := testManager(fakeDetector{result: wakeword.Result{Activated: true}}, azure, tx)
	if err := m.HandleTransmission(context.Background(), testTransmission("alice")); err != nil {
		t.Fatal(err)
	}
	m.End(context.Background(), "test")
	if !azure.session.closed {
		t.Fatal("expected session closed")
	}
	if got := m.Snapshot().State; got != StateIdle {
		t.Fatalf("state = %s", got)
	}
}

// slowFakeSession simulates a tool call (e.g. Foundry/Fabric IQ retrieval)
// that takes longer than the conversation idle-timeout to complete.
type slowFakeSession struct {
	id      string
	started chan struct{}
	release chan struct{}
	closed  bool
}

func (s *slowFakeSession) ID() string { return s.id }
func (s *slowFakeSession) AskAudio(ctx context.Context, pcm []int16, sampleRate int, interim ToolInterims) ([]int16, int, error) {
	close(s.started)
	select {
	case <-s.release:
		return []int16{1, 2, 3}, audio.AzureSampleRate, nil
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}
}
func (s *slowFakeSession) AskText(ctx context.Context, text string) (string, error) {
	return "", nil
}
func (s *slowFakeSession) Close(context.Context) error {
	s.closed = true
	return nil
}

type slowAzureFactory struct {
	session *slowFakeSession
}

func (f *slowAzureFactory) NewSession(context.Context) (AzureSession, error) {
	return f.session, nil
}

// TestBusyConversationNotExpiredDuringToolCall reproduces the bug where the
// idle-timeout watchdog (RunExpiryLoop) closed the session out from under an
// in-flight tool call (e.g. a slow Fabric IQ retrieve), because LastActivity
// is only refreshed after AskAudio/AskText returns. A tool call slower than
// the idle timeout must not be treated as conversation inactivity.
func TestBusyConversationNotExpiredDuringToolCall(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	session := &slowFakeSession{id: "sess-test", started: started, release: release}
	azure := &slowAzureFactory{session: session}
	tx := &fakeTX{}
	// A short idle timeout so the watchdog would fire almost immediately if
	// it didn't respect the Busy flag.
	m := NewManager(50*time.Millisecond, "mira-bot", "MIRA,OPERATOR", fakeDetector{result: wakeword.Result{Activated: true}}, audio.LinearResampler{}, azure, tx, slog.Default(), &metrics.Metrics{})

	errCh := make(chan error, 1)
	go func() {
		errCh <- m.HandleTransmission(context.Background(), testTransmission("alice"))
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("tool call (AskAudio) never started")
	}

	// Run the real expiry watchdog for a few ticks (>> the 50ms idle
	// timeout) while the tool call is still in flight.
	loopCtx, cancel := context.WithCancel(context.Background())
	loopDone := make(chan struct{})
	go func() {
		m.RunExpiryLoop(loopCtx)
		close(loopDone)
	}()
	time.Sleep(2500 * time.Millisecond)

	if snap := m.Snapshot(); snap.State != StateActive {
		t.Fatalf("conversation expired while tool call was in flight: state = %s", snap.State)
	}
	if session.closed {
		t.Fatal("session was closed while tool call was in flight")
	}

	close(release)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}

	if snap := m.Snapshot(); snap.State != StateActive {
		t.Fatalf("state after tool call completed = %s", snap.State)
	}
	if session.closed {
		t.Fatal("session should remain open after a successful exchange")
	}

	// Shut down the watchdog goroutine; RunExpiryLoop unconditionally ends
	// the conversation on ctx.Done (real shutdown), which is expected and
	// unrelated to the Busy-flag behavior under test above.
	cancel()
	<-loopDone
}

func TestTextActivationAndFollowupUseSameSession(t *testing.T) {
	azure := &fakeAzureFactory{}
	tx := &fakeTX{}
	m := testManager(fakeDetector{result: wakeword.Result{}}, azure, tx)
	if err := m.HandleTextMessage(context.Background(), TextMessage{Speaker: "alice", Text: "MIRA tell me a joke"}); err != nil {
		t.Fatal(err)
	}
	if got := m.Snapshot().State; got != StateActive {
		t.Fatalf("state = %s", got)
	}
	if err := m.HandleTextMessage(context.Background(), TextMessage{Speaker: "alice", Text: "tell me another one"}); err != nil {
		t.Fatal(err)
	}
	if azure.sessions != 1 {
		t.Fatalf("sessions = %d", azure.sessions)
	}
	if azure.session.texts != 2 {
		t.Fatalf("texts = %d", azure.session.texts)
	}
	if tx.textCount != 2 {
		t.Fatalf("text replies = %d", tx.textCount)
	}
	if azure.session.lastSpeakerCtx != "alice" {
		t.Fatalf("lastSpeakerCtx = %q, want %q (Manager must attach the speaker to ctx so Foundry session-reuse can key on it)", azure.session.lastSpeakerCtx, "alice")
	}
}

func TestTextActivationUsesConfiguredWakeWord(t *testing.T) {
	azure := &fakeAzureFactory{}
	tx := &fakeTX{}
	m := testManager(fakeDetector{result: wakeword.Result{}}, azure, tx)
	if err := m.HandleTextMessage(context.Background(), TextMessage{Speaker: "alice", Text: "Operator, get me an exit"}); err != nil {
		t.Fatal(err)
	}
	if azure.sessions != 1 {
		t.Fatalf("sessions = %d", azure.sessions)
	}
	if azure.session.texts != 1 {
		t.Fatalf("texts = %d", azure.session.texts)
	}
}

func TestInactiveTextIgnored(t *testing.T) {
	azure := &fakeAzureFactory{}
	tx := &fakeTX{}
	m := testManager(fakeDetector{result: wakeword.Result{}}, azure, tx)
	if err := m.HandleTextMessage(context.Background(), TextMessage{Speaker: "alice", Text: "tell me a joke"}); err != nil {
		t.Fatal(err)
	}
	if azure.sessions != 0 || tx.textCount != 0 {
		t.Fatalf("unexpected sessions=%d replies=%d", azure.sessions, tx.textCount)
	}
}

func TestSelfTextIgnored(t *testing.T) {
	azure := &fakeAzureFactory{}
	tx := &fakeTX{}
	m := testManager(fakeDetector{result: wakeword.Result{}}, azure, tx)
	if err := m.HandleTextMessage(context.Background(), TextMessage{Speaker: "mira-bot", Text: "MIRA tell me a joke"}); err != nil {
		t.Fatal(err)
	}
	if azure.sessions != 0 {
		t.Fatalf("sessions = %d", azure.sessions)
	}
}

func testManager(det wakeword.Detector, azure *fakeAzureFactory, tx *fakeTX) *Manager {
	return NewManager(45*time.Second, "mira-bot", "MIRA,OPERATOR", det, audio.LinearResampler{}, azure, tx, slog.Default(), &metrics.Metrics{})
}

func testTransmission(speaker string) Transmission {
	return Transmission{
		Channel:    "family",
		Speaker:    speaker,
		StreamID:   1,
		PCM:        make([]int16, 1600),
		SampleRate: audio.ZelloSampleRate,
		StartedAt:  time.Now(),
		EndedAt:    time.Now(),
	}
}
