# medspa-ai-platform

## Mission

AI-first medspa operations platform that captures missed leads, runs omni-channel conversations, books appointments, and collects deposits end-to-end. Our platform helps medical spas maximize revenue by automating lead capture, qualification, and conversion through intelligent multi-channel communication.

## Project Structure

```
medspa-ai-platform/
├── cmd/
│   └── api/                 # API server entry point
├── internal/
│   ├── api/                 # HTTP API layer
│   │   └── router/          # Chi router configuration
│   ├── config/              # Configuration and environment loaders
│   ├── leads/               # Lead management domain
│   ├── messaging/           # Messaging integrations (Twilio, etc.)
│   └── payments/            # Payment processing
├── pkg/
│   └── logging/             # Shared logging utilities
├── infra/
│   └── terraform/           # Infrastructure as code
└── .github/
    └── workflows/           # CI/CD pipelines
```

## Development Environment

### Prerequisites

- Go 1.22 or higher
- Task (task runner) - [Installation guide](https://taskfile.dev/installation/)
- Terraform 1.0+ (for infrastructure)
- Docker (optional, for local services)

### Setup

1. Clone the repository:
   ```bash
   git clone https://github.com/wolfman30/medspa-ai-platform.git
   cd medspa-ai-platform
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

3. Copy environment template:
   ```bash
   cp .env.example .env
   ```

4. Run tests:
   ```bash
   task test
   ```

5. Start the API server:
   ```bash
   task run
   ```

### Running with Docker + LocalStack

For full integration testing (API + Postgres + Redis + mocked AWS services), use Docker Compose:

```bash
cp .env.example .env        # if you haven't already
docker compose up --build   # or: task docker-up
```

This starts:

- Go API container (port 8080)
- PostgreSQL 15 (port 5432) with `medspa/medspa` credentials
- Redis 7 (port 6379)
- LocalStack (port 4566) emulating Secrets Manager/SQS/SNS/Lambda/CloudWatch Logs

Shut everything down with `docker compose down -v` (or `task docker-down`). When using LocalStack, point AWS SDK clients at `AWS_ENDPOINT_URL=http://localstack:4566` and use the dummy credentials already present in `.env.example`.

### Bootstrap Deployment (Lightsail + Neon) (deprecated)

This path is retained for historical reference only. New deployments should use ECS Fargate (Spot) + Lambda voice ingress + ElastiCache Redis:

- `docs/DEPLOYMENT_ECS.md`

### Available Tasks

Run `task --list` to see all available tasks:

- `task build` - Build the API server
- `task test` - Run all tests
- `task lint` - Run linters
- `task run` - Run the API server locally
- `task fmt` - Format code

## Production Environment

- `PORT` - HTTP server port (default: 8080)
- `ENV` - Environment name (development, staging, production)
- `LOG_LEVEL` - Logging level (debug, info, warn, error)
- `CORS_ALLOWED_ORIGINS` - Comma-separated allowlist for browser Origins (e.g. `https://aiwolfsolutions.com,https://wolfman30.github.io`)
- `SMS_PROVIDER` - `auto` (default), `telnyx`, or `twilio`; forces which outbound SMS provider workers should use when multiple credentials exist
- `DATABASE_URL` - PostgreSQL connection string
- `TELNYX_API_KEY` / `TELNYX_MESSAGING_PROFILE_ID` / `TELNYX_WEBHOOK_SECRET` - Telnyx Hosted Messaging creds
- `TELNYX_STOP_REPLY` / `TELNYX_HELP_REPLY` - Templates for STOP/HELP autoresponses
- `TELNYX_RETRY_MAX_ATTEMPTS` / `TELNYX_RETRY_BASE_DELAY` - Retry policy for the messaging worker
- `TELNYX_HOSTED_POLL_INTERVAL` - Poll cadence for hosted number orders
- `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_WEBHOOK_SECRET` - Existing Twilio integration (legacy inbound); leave `TWILIO_WEBHOOK_SECRET` empty to reuse the Auth Token for webhook validation
- `ADMIN_JWT_SECRET` - HMAC secret used to protect `/admin/*` endpoints
- `PAYMENT_PROVIDER_KEY` - Payment provider API key

### Architecture Reference

The detailed platform design lives in `docs/ARCHITECTURE_V3.md`. Keep it updated as components evolve.

### Deployment

The application is deployed on AWS using:

- **ECS/Fargate** - Primary API + worker services (always-on Go binaries)
- **Lambda (optional)** - Lightweight event/webhook processors
- **API Gateway / ALB** - External ingress
- **RDS PostgreSQL** - Managed database
- **VPC** - Network isolation
- **Secrets Manager** - Secure credential storage

Deployment is managed through Terraform:

```bash
cd infra/terraform
terraform init
terraform plan
terraform apply
```

### CI/CD

GitHub Actions automatically:

- Runs tests on every push
- Checks code formatting with `gofmt`
- Validates Terraform configurations
- Deploys to staging/production on merge to main

## API Endpoints

### Leads

- `POST /leads/web` - Capture web form lead submission

### Messaging (Telnyx)

Set `SMS_PROVIDER=twilio` to temporarily run outbound replies through your Twilio toll-free test number while keeping Telnyx credentials in place. Switch back to `telnyx` (or the default `auto` preference) once 10DLC registration is approved so hosted customer numbers are used in production.

- `POST /admin/hosted/orders` – start a hosted messaging order for a clinic (requires admin JWT)
- `POST /admin/10dlc/brands` / `/admin/10dlc/campaigns` – onboard 10DLC brand + campaign metadata
- `POST /admin/messages:send` – send SMS/MMS via Telnyx with quiet-hours + STOP enforcement
- `POST /webhooks/telnyx/messages` – inbound message + delivery receipt webhook (signature validated, idempotent)
- `POST /webhooks/telnyx/hosted` – hosted order status webhooks
- `GET /admin/clinics/{orgID}/stats` – per-clinic Revenue MVP counters (requires admin JWT)
- `GET /admin/clinics/{orgID}/dashboard` – missed-call conversion + LLM latency snapshot (requires admin JWT)
- `GET /metrics` – Prometheus metrics (`medspa_messaging_*` counters/histograms)

Run `make run-worker` (or deploy `cmd/messaging-worker`) alongside the API to poll hosted orders and retry failed outbound sends.

Use `scripts/check_package_coverage.sh` or `make ci-cover` to ensure all new messaging packages stay above the 90% coverage gate enforced in CI.

### Messaging

- `POST /messaging/twilio/webhook` - Handle incoming Twilio messages

## Contributing

1. Create a feature branch
2. Make your changes
3. Run tests and linters
4. Submit a pull request

## License

Proprietary - All rights reserved
