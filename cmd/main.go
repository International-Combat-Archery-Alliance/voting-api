package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/International-Combat-Archery-Alliance/auth/token"
	"github.com/International-Combat-Archery-Alliance/captcha/cfturnstile"
	"github.com/International-Combat-Archery-Alliance/telemetry"
	"github.com/International-Combat-Archery-Alliance/voting-api/api"
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("github.com/International-Combat-Archery-Alliance/voting-api/cmd")

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := getApiEnvironment()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	licenseKey, err := getNewRelicLicenseKey(ctx, env)
	if err != nil {
		logger.Error("failed to get New Relic license key", "error", err)
		os.Exit(1)
	}

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "otlp.nr-data.net:4317"
	}

	traceShutdown, flushTraces, err := telemetry.Init(ctx, telemetry.Options{
		ServiceName: "voting-api",
		Endpoint:    endpoint,
		APIKey:      licenseKey,
		Lambda:      telemetry.LambdaInfoFromEnv(),
	})
	if err != nil {
		logger.Error("failed to initialize telemetry", "error", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := traceShutdown(shutdownCtx); err != nil {
			logger.Error("failed to shutdown telemetry", "error", err)
		}
	}()

	// Start a root trace span for startup
	ctx, span := tracer.Start(ctx, "startup")

	var db api.DB
	if err := telemetry.RunWithSpan(ctx, tracer, "init-db", func(ctx context.Context) error {
		var err error
		db, err = makeDB(ctx)
		return err
	}); err != nil {
		span.RecordError(err)
		logger.Error("Error creating db client", "error", err)
		os.Exit(1)
	}

	var cfg *AppConfig
	if err := telemetry.RunWithSpan(ctx, tracer, "init-app-config", func(ctx context.Context) error {
		var err error
		cfg, err = fetchAppConfig(ctx, env)
		return err
	}); err != nil {
		span.RecordError(err)
		logger.Error("failed to fetch app config", "error", err)
		os.Exit(1)
	}

	validator := token.NewKeyCache(cfg.JWKSURL)
	if err := validator.StartupFetch(ctx); err != nil {
		logger.Warn("jwks startup fetch failed (non-fatal); user token verification will fail closed until keys are fetched", "error", err)
	}

	httpClient := &http.Client{Timeout: 5 * time.Second}
	cfTurnstileValidator := cfturnstile.NewValidator(httpClient, cfg.TurnstileSecretKey)

	votingAPI := api.NewAPI(db, logger, env, validator, cfTurnstileValidator, flushTraces)

	// End startup span after initialization completes
	span.End()

	serverSettings := getServerSettingsFromEnv()

	sigCtx, sigStop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer sigStop()

	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- votingAPI.ListenAndServe(serverSettings.Host, serverSettings.Port)
	}()

	select {
	case <-sigCtx.Done():
		logger.Info("shutting down gracefully")
	case err := <-serverErrCh:
		logger.Error("error running server", "error", err)
		os.Exit(1)
	}
}

type ServerSettings struct {
	Host string
	Port string
}

func getServerSettingsFromEnv() ServerSettings {
	return ServerSettings{
		Host: getEnvOrDefault("HOST", "0.0.0.0"),
		Port: getEnvOrDefault("PORT", "8080"),
	}
}
