package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/International-Combat-Archery-Alliance/auth"
	"github.com/International-Combat-Archery-Alliance/auth/token"
	"github.com/International-Combat-Archery-Alliance/middleware"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	nethttpMiddleware "github.com/oapi-codegen/nethttp-middleware"
)

const (
	accessTokenCookieKey = "ICAA_ACCESS_TOKEN"
)

var scopeValidators map[string]func(tok auth.AuthToken) error = map[string]func(tok auth.AuthToken) error{
	"admin": func(tok auth.AuthToken) error {
		if !slices.Contains(tok.Roles(), auth.RoleAdmin) {
			return fmt.Errorf("user is not an admin")
		}
		return nil
	},
}

func validateScopes(tok auth.AuthToken, scopes []string) error {
	for _, scope := range scopes {
		validator, ok := scopeValidators[scope]
		if !ok {
			return fmt.Errorf("unknown scope: %q", scope)
		}

		err := validator(tok)
		if err != nil {
			return fmt.Errorf("user does not have scope %q", scope)
		}
	}

	return nil
}

func (a *API) openapiValidateMiddleware(swagger *openapi3.T) middleware.MiddlewareFunc {
	return nethttpMiddleware.OapiRequestValidatorWithOptions(swagger, &nethttpMiddleware.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc: func(ctx context.Context, ai *openapi3filter.AuthenticationInput) error {
				logger := a.getLoggerOrBaseLogger(ctx)

				var tokenString string

				switch ai.SecuritySchemeName {
				case "icaaCookieAuth":
					authCookie, err := ai.RequestValidationInput.Request.Cookie(accessTokenCookieKey)
					if err != nil {
						return fmt.Errorf("auth token was not found in cookie %q", accessTokenCookieKey)
					}
					tokenString = authCookie.Value
				case "icaaBearerAuth":
					authHeader := ai.RequestValidationInput.Request.Header.Get("Authorization")
					if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
						return fmt.Errorf("auth token was not found in Authorization header")
					}
					tokenString = strings.TrimPrefix(authHeader, "Bearer ")
				default:
					return fmt.Errorf("unsupported security scheme: %s", ai.SecuritySchemeName)
				}

				claims, err := a.validator.ValidateUserAccessToken(ctx, tokenString)
				if err != nil {
					logger.Error("invalid jwt", slog.String("error", err.Error()))
					return fmt.Errorf("jwt is not valid")
				}

				// Create auth token from claims
				authToken := token.NewICAAAuthToken(claims)

				err = validateScopes(authToken, ai.Scopes)
				if err != nil {
					logger.Error("user attempted to hit an authenticated API without permissions", slog.String("error", err.Error()))
					return fmt.Errorf("user does not have access to scope")
				}

				loggerWithJwt := logger.With(slog.String("user-email", authToken.UserEmail()))
				ctx = middleware.CtxWithJWT(ctx, authToken)
				ctx = middleware.CtxWithLogger(ctx, loggerWithJwt)

				*ai.RequestValidationInput.Request = *ai.RequestValidationInput.Request.WithContext(ctx)

				return nil
			},
		},
		ErrorHandlerWithOpts: func(ctx context.Context, err error, w http.ResponseWriter, r *http.Request, opts nethttpMiddleware.ErrorHandlerOpts) {
			logger := a.getLoggerOrBaseLogger(ctx)

			var e Error

			var requestErr *openapi3filter.RequestError
			var secErr *openapi3filter.SecurityRequirementsError
			if errors.As(err, &requestErr) {
				e = Error{
					Message: err.Error(),
					Code:    InputValidationError,
				}
			} else if errors.As(err, &secErr) {
				e = Error{
					Message: err.Error(),
					Code:    AuthError,
				}
			} else {
				e = Error{
					Message: err.Error(),
					Code:    InternalError,
				}
			}
			jsonBody, err := json.Marshal(&e)
			if err != nil {
				logger.Error("failed to marshal input validation error resp", "error", err)
				jsonBody = []byte("{\"message\": \"input is invalid\", \"code\": \"InputValidationError\"}")
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(opts.StatusCode)
			w.Write(jsonBody)
		},
	})
}
