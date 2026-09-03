//go:generate go tool oapi-codegen --config openapi-codegen-config.yaml ../spec/api.yaml
package api

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/International-Combat-Archery-Alliance/auth/token"
	"github.com/International-Combat-Archery-Alliance/captcha"
	"github.com/International-Combat-Archery-Alliance/middleware"
	"github.com/International-Combat-Archery-Alliance/voting-api/polls"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

type Environment int

const (
	LOCAL Environment = iota
	PROD
)

type DB interface {
	polls.Repository
}

// UserTokenValidator verifies user access tokens against the login JWKS endpoint.
type UserTokenValidator interface {
	ValidateUserAccessToken(ctx context.Context, tokenString string) (*token.ICAAClaims, error)
}

type API struct {
	db     DB
	logger *slog.Logger
	env    Environment
	tracer trace.Tracer

	validator        UserTokenValidator
	captchaValidator captcha.Validator
	flushTraces      func(context.Context) error

	pollCache sync.Map
}

var _ StrictServerInterface = (*API)(nil)

func NewAPI(
	db DB,
	logger *slog.Logger,
	env Environment,
	validator UserTokenValidator,
	captchaValidator captcha.Validator,
	flushTraces func(context.Context) error,
) *API {
	return &API{
		db:               db,
		logger:           logger,
		env:              env,
		tracer:           otel.Tracer("github.com/International-Combat-Archery-Alliance/voting-api/api"),
		validator:        validator,
		captchaValidator: captchaValidator,
		flushTraces:      flushTraces,
	}
}

func (a *API) ListenAndServe(host string, port string) error {
	swagger, err := GetSwagger()
	if err != nil {
		return fmt.Errorf("Error loading swagger spec: %w", err)
	}

	swagger.Servers = nil

	strictHandler := NewStrictHandler(a, []StrictMiddlewareFunc{})

	r := http.NewServeMux()

	HandlerFromMux(strictHandler, r)

	swaggerUIMiddleware, err := middleware.HostSwaggerUI("/voting", swagger)
	if err != nil {
		return fmt.Errorf("failed to create swagger ui middleware: %w", err)
	}

	// Setup CORS middleware
	corsConfig := middleware.DefaultCorsConfig()
	corsConfig.IsProduction = a.env == PROD
	corsMiddleware := middleware.CorsMiddleware(corsConfig)

	middlewares := []middleware.MiddlewareFunc{
		// Executes from the bottom up
		a.openapiValidateMiddleware(swagger),
		corsMiddleware,
		swaggerUIMiddleware,
		middleware.AccessLogging(a.logger),
		middleware.OTELHandler,
		middleware.FlushTraces(a.flushTraces, a.logger, 3*time.Second),
	}

	if a.env == PROD {
		middlewares = append(middlewares, middleware.BaseNamePrefix(a.logger, "/voting"))
	}

	h := middleware.UseMiddlewares(r, middlewares...)

	s := &http.Server{
		Handler: h,
		Addr:    net.JoinHostPort(host, port),
	}

	return s.ListenAndServe()
}

func (a *API) getLoggerOrBaseLogger(ctx context.Context) *slog.Logger {
	logger, ok := middleware.GetLoggerFromCtx(ctx)
	if !ok {
		a.logger.Error("tried to get logger and it wasn't in the context")
		return a.logger
	}
	return logger
}

// isAdmin checks if the user in the context is an admin
func (a *API) isAdmin(ctx context.Context) bool {
	jwt, ok := middleware.GetJWTFromCtx(ctx)
	if !ok {
		return false
	}
	return jwt.IsAdmin()
}

func (a *API) getCachedPoll(ctx context.Context, id uuid.UUID) (polls.Poll, error) {
	if cached, ok := a.pollCache.Load(id); ok {
		return cached.(polls.Poll), nil
	}
	poll, err := a.db.GetPoll(ctx, id)
	if err != nil {
		return polls.Poll{}, err
	}
	a.pollCache.Store(id, poll)
	return poll, nil
}
