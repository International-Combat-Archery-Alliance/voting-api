# ICAA Voting API

An API for creating polls and voting on them, such as MVP votes for matches. Built with Go and deployed as an AWS Lambda function using AWS SAM.

## Tech Stack

- **Language**: Go 1.25+
- **Infrastructure**: AWS Lambda, API Gateway, DynamoDB
- **Deployment**: AWS SAM (Serverless Application Model)
- **API Spec**: OpenAPI 3.0 with code generation via [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen)
- **Authentication**: Google OAuth JWT for admin endpoints (cookie and bearer token)
- **Bot Protection**: Cloudflare Turnstile CAPTCHA on vote submissions

## Overview

- Admins create and manage polls with start/end times, rich option groups (e.g. teams with colors/logos), and per-poll configuration for results visibility and ballot selection limits.
- Anyone can vote on an active poll — no login required. Each vote is protected by a Turnstile CAPTCHA and a client generated idempotency key that prevents accidental double counting from retried requests.
- Results are gated by the poll's visibility setting: `Live`, `AfterClose`, or `AdminOnly`.

See `spec/api.yaml` for the full API specification.

## Prerequisites

- Go 1.25+
- [AWS SAM CLI](https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/install-sam-cli.html)
- Docker
- AWS CLI (configured with appropriate credentials for deployment)

## Local Development

1. **Start shared infrastructure**:
   Shared infrastructure (DynamoDB, Jaeger) is managed in `icaa.world/docker-compose.yml`.
   ```bash
   cd ../icaa.world && docker compose up -d
   ```

2. **Build and run the API locally**:

   ```bash
   make local
   ```

   This will:
   - Generate API code from the OpenAPI spec
   - Build the SAM application
   - Start the local API server

The local API will be available at `http://localhost:3006`. Swagger UI is at `http://localhost:3006/voting`.

Local mode uses Cloudflare's always-pass Turnstile test keys and a local JWT signing key, so votes and admin endpoints work out of the box.

## Building

```bash
make build
```

This generates the API code from the OpenAPI spec and builds the SAM application.

## Testing

```bash
go test ./...
```

DynamoDB tests run against a local DynamoDB via testcontainers (or DynamoDB Local on port 8000 when `TEST_IN_CI` is set).

## Deployment

The API is deployed via AWS SAM. The CI pipeline is configured in `.github/workflows/go.yml`.

For the first manual deployment (also creates the ECR repository and updates `samconfig.toml`):

```bash
sam deploy --guided
```

## Configuration

| Environment Variable         | Description                             | Default     |
| ---------------------------- | --------------------------------------- | ----------- |
| `DYNAMO_TABLE_NAME`          | DynamoDB table name                     | `voting-api`|
| `HOST`                       | Server host                             | `0.0.0.0`   |
| `PORT`                       | Server port                             | `8080`      |
| `OTEL_EXPORTER_OTLP_ENDPOINT`| OTLP traces endpoint                    | New Relic   |
| `JWT_SIGNING_KEY`            | Local-only JWT signing key              | dev default |
| `NEW_RELIC_LICENSE_KEY`      | Local-only New Relic license key        | unset       |

In production, secrets are fetched from SSM Parameter Store (`/jwtSigningKeys`, `/cfTurnstileSecretKey`, `/newrelic-license-key`).

## License

See [LICENSE](LICENSE) for details.
