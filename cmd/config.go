package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/International-Combat-Archery-Alliance/telemetry"
	"github.com/International-Combat-Archery-Alliance/voting-api/api"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"go.opentelemetry.io/otel/codes"
)

const (
	newRelicLicenseEnvVar  = "NEW_RELIC_LICENSE_KEY"
	newRelicLicenseSSMPath = "/newrelic-license-key"
)

var (
	awsCfg     aws.Config
	awsCfgErr  error
	awsCfgOnce sync.Once
)

// loadAWSConfig loads the AWS config once and caches it. Safe for concurrent use.
func loadAWSConfig(ctx context.Context) (aws.Config, error) {
	awsCfgOnce.Do(func() {
		ctx, span := tracer.Start(ctx, "load-aws-config")
		defer span.End()

		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			awsCfgErr = fmt.Errorf("unable to load AWS SDK config: %w", err)
			return
		}
		telemetry.InstrumentAWSConfig(&cfg)
		awsCfg = cfg
	})
	return awsCfg, awsCfgErr
}

// getSSMParameter fetches a single parameter from AWS Parameter Store (for New Relic key,
// which must be fetched separately before telemetry init).
func getSSMParameter(ctx context.Context, name string) (string, error) {
	cfg, err := loadAWSConfig(ctx)
	if err != nil {
		return "", err
	}

	client := ssm.NewFromConfig(cfg)
	result, err := client.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(name),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return "", fmt.Errorf("failed to get parameter %q: %w", name, err)
	}

	return aws.ToString(result.Parameter.Value), nil
}

// getSSMParameters fetches multiple parameters in a single API call.
// Returns a map of parameter name to value.
func getSSMParameters(ctx context.Context, names []string) (map[string]string, error) {
	if len(names) == 0 {
		return nil, nil
	}

	cfg, err := loadAWSConfig(ctx)
	if err != nil {
		return nil, err
	}

	client := ssm.NewFromConfig(cfg)
	result, err := client.GetParameters(ctx, &ssm.GetParametersInput{
		Names:          names,
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get parameters %v: %w", names, err)
	}

	if len(result.InvalidParameters) > 0 {
		return nil, fmt.Errorf("invalid parameters: %v", result.InvalidParameters)
	}

	params := make(map[string]string, len(result.Parameters))
	for _, p := range result.Parameters {
		params[aws.ToString(p.Name)] = aws.ToString(p.Value)
	}

	return params, nil
}

// AppConfig holds all configuration values needed to initialize services.
type AppConfig struct {
	// JWKSURL is the login JWKS endpoint used to verify user tokens.
	JWKSURL            string
	TurnstileSecretKey string
}

// fetchAppConfig retrieves all application configuration.
// In local mode, returns values from environment variables / defaults.
// In production, fetches all parameters in a single batched SSM GetParameters call.
func fetchAppConfig(ctx context.Context, env api.Environment) (*AppConfig, error) {
	if env == api.LOCAL {
		return localAppConfig()
	}
	return fetchProdAppConfig(ctx)
}

func localAppConfig() (*AppConfig, error) {
	return &AppConfig{
		JWKSURL: jwksURLForEnv(api.LOCAL),
		// Cloudflare's always-pass test secret key
		TurnstileSecretKey: "1x0000000000000000000000000000000AA",
	}, nil
}

func fetchProdAppConfig(ctx context.Context) (*AppConfig, error) {
	ssmNames := []string{
		"/cfTurnstileSecretKey",
	}

	params, err := getSSMParameters(ctx, ssmNames)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch app config from SSM: %w", err)
	}

	cfg := &AppConfig{
		JWKSURL: jwksURLForEnv(api.PROD),
	}

	if v, ok := params["/cfTurnstileSecretKey"]; ok {
		cfg.TurnstileSecretKey = v
	} else {
		return nil, fmt.Errorf("missing SSM parameter: /cfTurnstileSecretKey")
	}

	return cfg, nil
}

// jwksURLForEnv returns the login JWKS endpoint used to verify user tokens.
// LOGIN_JWKS_URL overrides both environments.
func jwksURLForEnv(env api.Environment) string {
	if u := os.Getenv("LOGIN_JWKS_URL"); u != "" {
		return u
	}
	if env == api.LOCAL {
		return "http://localhost:3001/login/.well-known/jwks.json"
	}
	return "https://api.icaa.world/login/.well-known/jwks.json"
}

// getNewRelicLicenseKey retrieves the New Relic license key.
// Must be fetched before telemetry.Init, so it uses a separate SSM call.
func getNewRelicLicenseKey(ctx context.Context, env api.Environment) (string, error) {
	if env == api.LOCAL {
		return os.Getenv(newRelicLicenseEnvVar), nil
	}
	return getSSMParameter(ctx, newRelicLicenseSSMPath)
}

func getApiEnvironment() api.Environment {
	if isLocal() {
		return api.LOCAL
	}
	return api.PROD
}

func isLocal() bool {
	return getEnvOrDefault("AWS_SAM_LOCAL", "false") == "true"
}

func getEnvOrDefault(key, defaultVal string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return defaultVal
}
