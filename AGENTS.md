# Agent Guidelines for Voting Service

This is a Go-based voting backend service using AWS SAM, DynamoDB, and Cloudflare Turnstile for bot protection.

## Build Commands

```bash
# Build the project (generates code and builds SAM app)
make build

# Run locally (builds, starts SAM local API on port 3006)
make local

# Generate Go code from OpenAPI spec
go generate ./...

# Run all tests
go test ./...

# Run tests for a specific package
go test ./polls/...
go test ./dynamo/...
go test ./api/...

# Run a single test
go test -run TestValidateBallot ./polls/...
go test -run TestRecordVote ./dynamo/...

# Run tests with verbose output
go test -v ./...
```

## Code Style Guidelines

### Imports
- Group imports: stdlib first, then third-party, then internal packages
- Separate groups with blank lines
- Use `goimports` or `gofmt` for formatting

### Naming Conventions
- **Types**: PascalCase (e.g., `Poll`, `VoteRecord`, `Repository`)
- **Interfaces**: PascalCase with descriptive names (e.g., `Repository`)
- **Functions**: PascalCase for exported, camelCase for unexported
- **Constants**: UPPER_SNAKE_CASE for error reasons (e.g., `REASON_POLL_NOT_ACTIVE`)
- **Variables**: camelCase (e.g., `pollID`, `votingRepo`)
- **Files**: lowercase with underscores avoided (e.g., `polls.go`, `error.go`)

### Error Handling
- Use custom error types with `ErrorReason` constants (see `polls/error.go`)
- Always wrap errors with context using `fmt.Errorf("...: %w", err)`
- Implement `Unwrap() error` for error chaining
- Check errors using `errors.As()` for type assertions

### Types and Testing
- Use table-driven tests with `t.Run()` for subtests
- Mock external dependencies using struct-based mocks
- Use `testify/assert` for assertions
- Test files should be named `*_test.go`

### Architecture Patterns (Hexagonal/Ports and Adapters)

This codebase follows **Hexagonal Architecture** (Ports and Adapters pattern) to separate business logic from infrastructure concerns:

**Layer Structure:**
```
cmd/           - Entry point, wires dependencies
api/           - Driving adapters (HTTP handlers, middleware)
polls/         - Domain: Poll aggregate, vote/ballot business logic, repository port
dynamo/        - Driven adapters (DynamoDB repository implementations)
```

**Key Principles:**
- **Ports**: Interfaces defined in domain packages (e.g., `polls.Repository`)
- **Driving Adapters**: `api/` handlers that call domain logic
- **Driven Adapters**: `dynamo/` implements the repository interface
- **Dependency Direction**: Domain depends on nothing; infrastructure depends on domain interfaces
- **Dependency Injection**: All dependencies passed via constructors (`NewAPI()`, `NewDB()`)

**Testing Implications:**
- Domain logic tested with mock implementations of ports
- No external dependencies needed for unit tests
- Example: `api/votes_test.go` mocks `DB` and the captcha validator

### Domain Rules
- Votes are only accepted while a poll is active (`now` within `[startTime, endTime]`)
- A poll's `voteConfig` limits ballot size overall (`maxSelections`) and per group (`maxSelectionsPerGroup`)
- Results visibility (`Live`, `AfterClose`, `AdminOnly`) is enforced in the API layer via `Poll.CanViewResults`
- Vote idempotency: `POST /polls/{id}/votes` requires an `Idempotency-Key` header; the key is checked **before** captcha validation so retries short circuit. Records are stored with a 24h TTL.

### AWS SAM & Local Development
- Shared infrastructure (DynamoDB, Jaeger) is managed in `icaa.world/docker-compose.yml`
- Start shared infrastructure first: `cd icaa.world && docker compose up -d`
- Use `make local` for full local development environment
- SAM local API gateway runs on port 3006
- Environment variables in `env.json` for local config
- Local mode uses Cloudflare's always-pass Turnstile test key

### Data Model (single DynamoDB table)
- `PK=POLL#<id>, SK=#METADATA` — poll with embedded groups/options, `Version` for optimistic locking, `GSI1PK=POLLS`, `GSI1SK=POLL#<startTimeRFC3339>#<id>` for listing
- `PK=POLL#<id>, SK=RESULTS` — `{TotalVotes, Counts: {optionId: count}}`, created alongside the poll (DynamoDB cannot create nested map paths on demand) and incremented atomically per vote
- `PK=POLL#<id>, SK=IDEMPOTENCY#<key>` — idempotency record of a cast ballot with a TTL

### Code Generation
- OpenAPI spec generates server code: `//go:generate go tool oapi-codegen`
- Always run `go generate ./...` after modifying `spec/api.yaml`
