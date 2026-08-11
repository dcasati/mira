package azure

import (
	"context"
	"log/slog"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/dcasati/mira/internal/audio"
	"github.com/dcasati/mira/internal/conversation"
)

const TokenScope = "https://ai.azure.com/.default"

type Config struct {
	Endpoint   string
	Deployment string
	Voice      string
}

type Factory struct {
	cfg       Config
	cred      azcore.TokenCredential
	resampler audio.Resampler
	logger    *slog.Logger
}

func NewFactory(cfg Config, resampler audio.Resampler, logger *slog.Logger) (*Factory, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, err
	}
	cfg.Endpoint = strings.TrimRight(cfg.Endpoint, "/")
	return &Factory{cfg: cfg, cred: cred, resampler: resampler, logger: logger}, nil
}

func (f *Factory) NewSession(ctx context.Context) (conversation.AzureSession, error) {
	return newRealtimeSession(ctx, f.cfg, f.cred, f.resampler, f.logger)
}
