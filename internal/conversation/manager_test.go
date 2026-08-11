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
	id     string
	asks   int
	closed bool
}

func (s *fakeSession) ID() string { return s.id }
func (s *fakeSession) AskAudio(context.Context, []int16, int) ([]int16, int, error) {
	s.asks++
	return []int16{1, 2, 3}, audio.AzureSampleRate, nil
}
func (s *fakeSession) Close(context.Context) error {
	s.closed = true
	return nil
}

type fakeTX struct{ count int }

func (t *fakeTX) TransmitPCM(context.Context, []int16, int) error {
	t.count++
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

func testManager(det wakeword.Detector, azure *fakeAzureFactory, tx *fakeTX) *Manager {
	return NewManager(45*time.Second, "mira-bot", det, audio.LinearResampler{}, azure, tx, slog.Default(), &metrics.Metrics{})
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
