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

### Available Tasks

Run `task --list` to see all available tasks:

- `task build` - Build the API server
- `task test` - Run all tests
- `task lint` - Run linters
- `task run` - Run the API server locally
- `task fmt` - Format code

## Production Environment

### Environment Variables

The application uses the following environment variables:

- `PORT` - HTTP server port (default: 8080)
- `ENV` - Environment name (development, staging, production)
- `LOG_LEVEL` - Logging level (debug, info, warn, error)
- `DATABASE_URL` - PostgreSQL connection string
- `TWILIO_ACCOUNT_SID` - Twilio account identifier
- `TWILIO_AUTH_TOKEN` - Twilio authentication token
- `TWILIO_WEBHOOK_SECRET` - Secret for webhook signature verification
- `PAYMENT_PROVIDER_KEY` - Payment provider API key

### Deployment

The application is deployed on AWS using:

- **API Gateway + Lambda** - Serverless API hosting
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

### Messaging

- `POST /messaging/twilio/webhook` - Handle incoming Twilio messages

## Contributing

1. Create a feature branch
2. Make your changes
3. Run tests and linters
4. Submit a pull request

## License

Proprietary - All rights reserved
