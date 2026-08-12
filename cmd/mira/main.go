package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/dcasati/mira/internal/audio"
	miraazure "github.com/dcasati/mira/internal/azure"
	"github.com/dcasati/mira/internal/config"
	"github.com/dcasati/mira/internal/conversation"
	"github.com/dcasati/mira/internal/health"
	"github.com/dcasati/mira/internal/metrics"
	"github.com/dcasati/mira/internal/wakeword"
	"github.com/dcasati/mira/internal/zello"
)

type readiness struct {
	zello    *zello.Client
	detector wakeword.Detector
}

func (r readiness) Ready() bool {
	return r.zello != nil && r.zello.Ready() && r.detector != nil && r.detector.Ready()
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	cfg, err := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	if err != nil {
		logger.Error("config.invalid", "error", err)
		os.Exit(2)
	}
	slog.SetDefault(logger)

	resampler := audio.LinearResampler{}
	detector, err := wakeword.NewWhisperDetector(cfg.WhisperModelPath, cfg.WakeWord)
	if err != nil {
		logger.Error("wakeword.load_failed", "error", err)
		os.Exit(1)
	}
	defer detector.Close()
	logger.Info("wakeword.model_loaded")

	if cfg.AudioFile != "" {
		if err := runAudioFile(ctx, cfg.AudioFile, detector, resampler, logger); err != nil {
			logger.Error("dev_audio.failed", "error", err)
			os.Exit(1)
		}
		return
	}

	m := &metrics.Metrics{}
	zelloClient := zello.NewClient(zello.Config{
		Endpoint:     cfg.ZelloEndpoint,
		Username:     cfg.ZelloUsername,
		Password:     cfg.ZelloPassword,
		Channel:      cfg.ZelloChannel,
		AuthToken:    cfg.ZelloAuthToken,
		MaxRXSeconds: cfg.MaxRXSeconds,
		MaxTXSeconds: cfg.MaxTXSeconds,
	}, zello.NewOpusFactory(), resampler, logger, m)
	azureFactory, err := miraazure.NewFactory(miraazure.Config{
		Endpoint:   cfg.AzureOpenAIEndpoint,
		Deployment: cfg.AzureOpenAIRealtimeDeployment,
		Voice:      cfg.AzureVoice,
	}, resampler, logger)
	if err != nil {
		logger.Error("azure.credential_failed", "error", err)
		os.Exit(1)
	}
	manager := conversation.NewManager(cfg.ConversationTimeout, cfg.ZelloUsername, detector, resampler, azureFactory, zelloClient, logger, m)
	healthServer := health.New(cfg.HTTPListenAddr, readiness{zello: zelloClient, detector: detector}, m.Handler(), logger)

	errCh := make(chan error, 4)
	go func() { errCh <- healthServer.Run(ctx) }()
	go func() { errCh <- zelloClient.Run(ctx) }()
	go manager.RunExpiryLoop(ctx)
	go func() {
		for {
			select {
			case <-ctx.Done():
				errCh <- nil
				return
			case t := <-zelloClient.Transmissions():
				if err := manager.HandleTransmission(ctx, t); err != nil && !errors.Is(err, context.Canceled) {
					m.ErrorsTotal.Add(1)
					logger.Error("conversation.handle_failed", "error", err, "speaker", t.Speaker)
				}
			case text := <-zelloClient.TextMessages():
				if err := manager.HandleTextMessage(ctx, text); err != nil && !errors.Is(err, context.Canceled) {
					m.ErrorsTotal.Add(1)
					logger.Error("conversation.text_handle_failed", "error", err, "speaker", text.Speaker)
				}
			}
		}
	}()

	logger.Info("service.started", "channel", cfg.ZelloChannel)
	err = <-errCh
	stop()
	manager.End(context.Background(), "shutdown")
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.Canceled) {
		logger.Error("service.stopped", "error", err)
		os.Exit(1)
	}
	logger.Info("service.shutdown")
}

func runAudioFile(ctx context.Context, path string, detector wakeword.Detector, resampler audio.Resampler, logger *slog.Logger) error {
	pcm, err := audio.ReadWAVPCM16Mono(path)
	if err != nil {
		return err
	}
	wakePCM, err := resampler.Resample(pcm.Samples, pcm.SampleRate, audio.WakeSampleRate)
	if err != nil {
		return err
	}
	result, err := detector.Detect(ctx, wakePCM, audio.WakeSampleRate)
	if err != nil {
		return err
	}
	logger.Info("dev_audio.result", "activated", result.Activated, "transcript", result.Transcript, "query", result.Query)
	fmt.Printf("activated=%t\ntranscript=%s\nquery=%s\n", result.Activated, result.Transcript, result.Query)
	return nil
}
