package zello

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dcasati/mira/internal/audio"
	"github.com/dcasati/mira/internal/backoff"
	"github.com/dcasati/mira/internal/conversation"
	"github.com/dcasati/mira/internal/metrics"
	"github.com/gorilla/websocket"
)

const (
	txStartAttempts = 3
	txStartBackoff  = 1500 * time.Millisecond
)

type Config struct {
	Endpoint     string
	Username     string
	Password     string
	Channel      string
	AuthToken    string
	MaxRXSeconds int
	MaxTXSeconds int
}

type Client struct {
	cfg      Config
	codec    CodecFactory
	resample audio.Resampler
	logger   *slog.Logger
	metrics  *metrics.Metrics

	connMu sync.RWMutex
	conn   *websocket.Conn

	seq     atomic.Uint32
	pending sync.Map
	writes  chan any
	rx      chan conversation.Transmission
	text    chan conversation.TextMessage
	streams sync.Map

	connected atomic.Bool
	joined    atomic.Bool
	rxActive  atomic.Int64
	txMu      sync.Mutex
}

type pendingCall struct {
	resp chan Response
}

func NewClient(cfg Config, codec CodecFactory, resample audio.Resampler, logger *slog.Logger, metrics *metrics.Metrics) *Client {
	return &Client{
		cfg:      cfg,
		codec:    codec,
		resample: resample,
		logger:   logger,
		metrics:  metrics,
		writes:   make(chan any, 64),
		rx:       make(chan conversation.Transmission, 16),
		text:     make(chan conversation.TextMessage, 16),
	}
}

func (c *Client) Ready() bool {
	return c.connected.Load() && c.joined.Load() && c.codec.Ready()
}

func (c *Client) Transmissions() <-chan conversation.Transmission { return c.rx }

func (c *Client) TextMessages() <-chan conversation.TextMessage { return c.text }

func (c *Client) Run(ctx context.Context) error {
	bo := backoff.New(time.Second, 30*time.Second)
	for ctx.Err() == nil {
		if err := c.connectOnce(ctx); err != nil && ctx.Err() == nil {
			c.metrics.ErrorsTotal.Add(1)
			delay := bo.Next()
			c.logger.Warn("zello.disconnected", "reason", err.Error(), "retry_in_ms", delay.Milliseconds())
			select {
			case <-time.After(delay):
			case <-ctx.Done():
			}
			continue
		}
		bo.Reset()
	}
	return ctx.Err()
}

func (c *Client) connectOnce(ctx context.Context) error {
	c.logger.Info("zello.connecting", "endpoint", c.cfg.Endpoint)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.cfg.Endpoint, http.Header{})
	if err != nil {
		return err
	}
	defer conn.Close()
	c.setConn(conn)
	defer c.clearConn()

	conn.SetPingHandler(func(data string) error {
		_ = conn.SetReadDeadline(time.Now().Add(75 * time.Second))
		return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(5*time.Second))
	})
	_ = conn.SetReadDeadline(time.Now().Add(75 * time.Second))

	errCh := make(chan error, 2)
	go func() { errCh <- c.writeLoop(ctx, conn) }()
	go func() { errCh <- c.readLoop(ctx, conn) }()

	if err := c.logon(ctx); err != nil {
		return err
	}
	c.connected.Store(true)
	c.metrics.ZelloConnected.Store(1)
	c.logger.Info("zello.authenticated", "channel", c.cfg.Channel)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		if err == nil {
			err = errors.New("zello loop ended")
		}
		return err
	}
}

func (c *Client) setConn(conn *websocket.Conn) {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	c.conn = conn
}

func (c *Client) clearConn() {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	c.conn = nil
	c.connected.Store(false)
	c.joined.Store(false)
	c.metrics.ZelloConnected.Store(0)
	c.pending.Range(func(k, v any) bool {
		if p, ok := v.(*pendingCall); ok {
			close(p.resp)
		}
		c.pending.Delete(k)
		return true
	})
	c.streams.Range(func(k, v any) bool {
		if s, ok := v.(*rxStream); ok && s.decoder != nil {
			_ = s.decoder.Close()
		}
		c.streams.Delete(k)
		return true
	})
}

func (c *Client) logon(ctx context.Context) error {
	req := LogonRequest{
		Command:      "logon",
		AuthToken:    c.cfg.AuthToken,
		Username:     c.cfg.Username,
		Password:     c.cfg.Password,
		Channels:     []string{c.cfg.Channel},
		Version:      "mira/0.1.0",
		PlatformType: "gateway",
		PlatformName: "MIRA Gateway",
	}
	resp, err := c.call(ctx, &req)
	if err != nil {
		return err
	}
	if resp.Error != "" || !resp.Success {
		return fmt.Errorf("zello logon failed: %s", resp.Error)
	}
	return nil
}

func (c *Client) call(ctx context.Context, req any) (Response, error) {
	seq := c.seq.Add(1)
	data, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil {
		return Response{}, err
	}
	msg["seq"] = float64(seq)
	p := &pendingCall{resp: make(chan Response, 1)}
	c.pending.Store(seq, p)
	defer c.pending.Delete(seq)
	select {
	case c.writes <- msg:
	case <-ctx.Done():
		return Response{}, ctx.Err()
	}
	select {
	case resp, ok := <-p.resp:
		if !ok {
			return Response{}, errors.New("zello connection closed")
		}
		return resp, nil
	case <-time.After(15 * time.Second):
		return Response{}, errors.New("zello request timed out")
	case <-ctx.Done():
		return Response{}, ctx.Err()
	}
}

func (c *Client) writeLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg := <-c.writes:
			switch v := msg.(type) {
			case []byte:
				if err := conn.WriteMessage(websocket.BinaryMessage, v); err != nil {
					return err
				}
			default:
				if err := conn.WriteJSON(v); err != nil {
					return err
				}
			}
		}
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		switch msgType {
		case websocket.TextMessage:
			if err := c.handleText(ctx, data); err != nil {
				return err
			}
		case websocket.BinaryMessage:
			if err := c.handleBinary(ctx, data); err != nil {
				c.logger.Warn("zello.rx_binary_error", "reason", err.Error())
				c.metrics.ErrorsTotal.Add(1)
			}
		}
	}
}

func (c *Client) handleText(ctx context.Context, data []byte) error {
	if resp, ok := IsResponse(data); ok {
		if p, exists := c.pending.Load(resp.Seq); exists {
			p.(*pendingCall).resp <- resp
			return nil
		}
	}
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}
	switch event.Command {
	case "on_channel_status":
		if event.Channel == c.cfg.Channel && event.Status == "online" {
			c.joined.Store(true)
			c.logger.Info("zello.channel_joined", "channel", event.Channel, "users_online", event.UsersOnline)
		}
		if event.Channel == c.cfg.Channel && event.Status == "offline" {
			c.joined.Store(false)
			c.logger.Warn("zello.channel_offline", "channel", event.Channel, "reason", event.Error)
		}
	case "on_stream_start":
		if event.Codec != "opus" || event.Type != "audio" {
			return nil
		}
		header, err := ParseCodecHeader(event.CodecHeader)
		if err != nil {
			return err
		}
		decoder, err := c.codec.NewDecoder(header.SampleRate)
		if err != nil {
			return err
		}
		c.rxActive.Add(1)
		c.streams.Store(event.StreamID, &rxStream{
			id:         event.StreamID,
			channel:    event.Channel,
			speaker:    event.From,
			sampleRate: header.SampleRate,
			startedAt:  time.Now(),
			decoder:    decoder,
			maxSamples: header.SampleRate * c.cfg.MaxRXSeconds,
		})
		c.logger.Info("zello.rx_started", "channel", event.Channel, "speaker", event.From, "stream_id", event.StreamID, "sample_rate", header.SampleRate, "frames_per_packet", header.FramesPerPacket, "frame_size_ms", header.FrameSizeMS, "packet_duration", event.PacketDuration)
	case "on_stream_stop":
		value, ok := c.streams.LoadAndDelete(event.StreamID)
		if !ok {
			return nil
		}
		c.rxActive.Add(-1)
		stream := value.(*rxStream)
		t := stream.finish()
		c.metrics.RXTransmissionsTotal.Add(1)
		c.logger.Info("zello.rx_finished", "speaker", t.Speaker, "stream_id", t.StreamID, "duration_ms", t.EndedAt.Sub(t.StartedAt).Milliseconds())
		select {
		case c.rx <- t:
		default:
			c.metrics.ErrorsTotal.Add(1)
			c.logger.Warn("zello.rx_dropped", "reason", "transmission_queue_full")
		}
	case "on_text_message":
		if event.Channel != c.cfg.Channel {
			return nil
		}
		msg := conversation.TextMessage{
			Channel:   event.Channel,
			Speaker:   event.From,
			MessageID: event.MessageID,
			Text:      event.Text,
			At:        time.Now(),
		}
		c.logger.Info("zello.text_received", "channel", msg.Channel, "speaker", msg.Speaker, "message_id", msg.MessageID)
		select {
		case c.text <- msg:
		default:
			c.metrics.ErrorsTotal.Add(1)
			c.logger.Warn("zello.text_dropped", "reason", "text_queue_full")
		}
	case "on_error":
		return fmt.Errorf("zello server error: %s", event.Error)
	default:
		_ = ctx
	}
	return nil
}

func (c *Client) TransmitText(ctx context.Context, text string) error {
	c.txMu.Lock()
	defer c.txMu.Unlock()
	if len(text) > 30*1024 {
		text = text[:30*1024]
	}
	msg := SendTextMessageRequest{
		Command: "send_text_message",
		Channel: c.cfg.Channel,
		Text:    text,
	}
	select {
	case c.writes <- msg:
	case <-ctx.Done():
		return ctx.Err()
	}
	c.metrics.TXTransmissionsTotal.Add(1)
	c.logger.Info("zello.text_sent", "channel", c.cfg.Channel, "bytes", len(text))
	return nil
}

func (c *Client) handleBinary(_ context.Context, data []byte) error {
	streamID, _, payload, err := DecodeAudioPacket(data)
	if err != nil {
		return err
	}
	value, ok := c.streams.Load(streamID)
	if !ok {
		return nil
	}
	return value.(*rxStream).append(payload)
}

func (c *Client) TransmitPCM(ctx context.Context, pcm []int16, sampleRate int) error {
	c.txMu.Lock()
	defer c.txMu.Unlock()
	for c.rxActive.Load() > 0 {
		c.logger.Info("zello.tx_waiting", "reason", "channel_busy")
		select {
		case <-time.After(250 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	zelloPCM, err := c.resample.Resample(pcm, sampleRate, audio.ZelloSampleRate)
	if err != nil {
		return err
	}
	maxSamples := audio.ZelloSampleRate * c.cfg.MaxTXSeconds
	if len(zelloPCM) > maxSamples {
		zelloPCM = zelloPCM[:maxSamples]
	}
	encoder, err := c.codec.NewEncoder(audio.ZelloSampleRate)
	if err != nil {
		return err
	}
	defer encoder.Close()
	resp, err := c.startTransmitStream(ctx)
	if err != nil {
		return err
	}
	c.logger.Info("zello.tx_started", "stream_id", resp.StreamID)
	samplesPerPacket := audio.ZelloSampleRate * audio.PacketMS / 1000
	for start := 0; start < len(zelloPCM); start += samplesPerPacket {
		end := start + samplesPerPacket
		if end > len(zelloPCM) {
			end = len(zelloPCM)
		}
		frame := make([]int16, samplesPerPacket)
		copy(frame, zelloPCM[start:end])
		opusPacket, err := encoder.Encode(frame)
		if err != nil {
			return err
		}
		select {
		case c.writes <- EncodeAudioPacket(resp.StreamID, opusPacket):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	stopResp, err := c.call(ctx, &StopStreamRequest{Command: "stop_stream", StreamID: resp.StreamID, Channel: c.cfg.Channel})
	if err != nil {
		return err
	}
	if stopResp.Error != "" {
		return fmt.Errorf("zello stop_stream failed: %s", stopResp.Error)
	}
	c.metrics.TXTransmissionsTotal.Add(1)
	c.logger.Info("zello.tx_finished", "stream_id", resp.StreamID)
	return nil
}

func (c *Client) startTransmitStream(ctx context.Context) (Response, error) {
	for attempt := 1; attempt <= txStartAttempts; attempt++ {
		resp, err := c.call(ctx, &StartStreamRequest{
			Command:        "start_stream",
			Channel:        c.cfg.Channel,
			Type:           "audio",
			Codec:          "opus",
			CodecHeader:    MakeCodecHeader(audio.ZelloSampleRate, 1, audio.PacketMS),
			PacketDuration: audio.PacketMS,
		})
		if err != nil {
			return Response{}, err
		}
		if resp.Error == "" && resp.StreamID != 0 {
			return resp, nil
		}
		if !isWoodpeckerProhibited(resp.Error) || attempt == txStartAttempts {
			return Response{}, fmt.Errorf("zello start_stream failed: %s", resp.Error)
		}
		c.logger.Warn("zello.tx_start_retry", "reason", resp.Error, "attempt", attempt+1, "retry_in_ms", txStartBackoff.Milliseconds())
		select {
		case <-time.After(txStartBackoff):
		case <-ctx.Done():
			return Response{}, ctx.Err()
		}
	}
	return Response{}, errors.New("zello start_stream failed")
}

func isWoodpeckerProhibited(err string) bool {
	return strings.Contains(strings.ToLower(err), "woodpecker prohibited")
}
