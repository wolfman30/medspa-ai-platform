# Deployment (AWS ECS Fargate Spot + Lambda Voice + ElastiCache Redis)

This is the step-by-step migration plan to move `medspa-ai-platform` off the Lightsail bootstrap deployment and onto:

- **ECS Fargate (Spot)** for the core Go API container (behind an **ALB**)
- **AWS Lambda** for voice-call webhooks (Twilio/Telnyx) as a sidecar ingress
- **Amazon ElastiCache for Redis** (managed Redis) instead of host/container Redis
- **Neon or RDS Postgres** (either works; choose one per environment)
- **AWS Secrets Manager** for sensitive config (API keys, DB DSN, Redis auth)

Implementation in this repo:

- Terraform: `infra/terraform`
- Voice webhook Lambda entrypoint: `cmd/voice-lambda`
- Lambda container image target: `Dockerfile` target `voice-lambda`

---

## 0) Current state (what we're replacing)

The bootstrap deployment (`docs/BOOTSTRAP_DEPLOYMENT.md`) runs the API/workers on **Lightsail** and uses:

- Neon Postgres (managed)
- Redis on the host (or via `docker-compose.bootstrap.yml`)

This path is deprecated for production. Lightsail + self-managed Redis is no longer acceptable.

---

## 1) Target architecture (high level)

1. **Networking**
   - One VPC per environment (`development`, `production`)
   - Public subnets for the ALB
   - Private subnets for ECS tasks + ElastiCache (and optionally RDS)
2. **Core API**
   - ECS cluster with **FARGATE_SPOT** (optional on-demand fallback)
   - ECS Service running the `api` container behind an ALB
   - **Blue/green deployments** via **CodeDeploy** (two target groups + an internal test listener)
3. **Voice webhooks**
   - API Gateway (HTTP API) -> Lambda (`voice-lambda`)
   - Lambda forwards voice webhook requests to the ECS API voice endpoints
4. **Redis**
   - ElastiCache Redis in private subnets
   - API gets `REDIS_ADDR` from Terraform output and `REDIS_PASSWORD` from Secrets Manager
5. **Secrets**
   - Secrets Manager stores `DATABASE_URL` + provider keys + JWT secret
6. **CI/CD**
   - GitHub Actions builds/pushes images to ECR and applies Terraform to `development`/`production`

---

## 2) Migration checklist (ordered, actionable)

### 2.1 Prerequisites / decisions

1. Pick AWS account + region (Terraform default: `us-east-1`).
2. Pick database per environment:
   - **Neon** (fastest): keep existing DSN; ECS tasks egress via NAT.
   - **RDS** (AWS-native): provision in the VPC (Aurora Serverless v2 recommended for prod if switching).
3. Plan ingress:
   - API traffic -> ALB
   - Voice webhooks -> API Gateway + Lambda
4. For **production blue/green**, provision an **ACM certificate** and set `api_certificate_arn` (CodeDeploy test listener uses HTTPS).

### 2.2 Terraform backend (state) setup

1. Create an S3 bucket for Terraform state (and optionally a DynamoDB lock table).
2. Initialize per environment using separate state keys:
   - `medspa-ai-platform/development/terraform.tfstate`
   - `medspa-ai-platform/production/terraform.tfstate`

Example:

```bash
cd infra/terraform
terraform init \
  -backend-config="bucket=<bucket>" \
  -backend-config="key=medspa-ai-platform/development/terraform.tfstate" \
  -backend-config="region=us-east-1"
```

### 2.3 Provision infrastructure (development first)

```bash
cd infra/terraform
terraform apply -var="environment=development"
```

Record outputs:

- `api_alb_dns_name` (core API base URL)
- `api_gateway_url` (voice webhook gateway base URL)
- `redis_endpoint` (managed Redis)

### 2.4 Configure Secrets Manager

Populate the `medspa-<environment>-app-secrets` secret with at least:

- `DATABASE_URL`
- `ADMIN_JWT_SECRET`
- Provider credentials (Twilio/Telnyx/Square/etc) as needed

Terraform creates placeholders (and ignores future secret edits), so you can safely edit values in AWS without Terraform overwriting them.

### 2.5 Build & deploy containers

Images to publish (same git SHA tag recommended):

- API: `Dockerfile` target `api` -> ECR repo `medspa-<environment>-api`
- DB migrator (one-off task): `Dockerfile` target `migrate` -> ECR repo `medspa-<environment>-api` tag `migrate-<gitsha>`
- Voice Lambda: `Dockerfile` target `voice-lambda` -> ECR repo `medspa-<environment>-voice-lambda`

Re-apply Terraform after pushing images (or force a new ECS deployment).

### 2.6 Run database migrations (required for fresh RDS)

Terraform provisions a one-off ECS task definition for running schema migrations:

- `terraform output -raw migration_task_definition_arn`

You also need:

- `terraform output -json private_subnet_ids`
- `terraform output -raw ecs_task_security_group_id`

Example (bash):

```bash
cd infra/terraform

CLUSTER="$(terraform output -raw ecs_cluster_name)"
TASK_DEF="$(terraform output -raw migration_task_definition_arn)"
SG="$(terraform output -raw ecs_task_security_group_id)"
SUBNET_1="$(terraform output -json private_subnet_ids | jq -r '.[0]')"
SUBNET_2="$(terraform output -json private_subnet_ids | jq -r '.[1]')"

aws ecs run-task \
  --cluster "$CLUSTER" \
  --task-definition "$TASK_DEF" \
  --launch-type FARGATE \
  --network-configuration "awsvpcConfiguration={subnets=[$SUBNET_1,$SUBNET_2],securityGroups=[$SG],assignPublicIp=DISABLED}"
```

### 2.7 Point voice providers at the Lambda gateway

- Twilio Voice webhook:
  - `POST {api_gateway_url}/webhooks/twilio/voice`
- Telnyx Voice webhook:
  - `POST {api_gateway_url}/webhooks/telnyx/voice`

The Lambda forwards these requests to the ECS API's existing voice endpoints.

### 2.8 Validate end-to-end (development)

1. `GET http://{api_alb_dns_name}/health` returns 200.
2. Outbox poller is healthy (no `outbox fetch failed` errors after running migrations).
3. Redis connectivity confirmed in API logs.
4. Trigger missed-call flow and verify conversation start behavior.

### 2.9 Repeat for production

1. Apply with `-var="environment=production"`.
2. Populate prod secrets.
3. Push prod images.
4. Update Twilio/Telnyx voice webhook URLs.

### 2.10 Rollback plan

- Keep Lightsail running until production is stable.
- Roll back by reverting webhook URLs + DNS to Lightsail.
- ECS rollback by redeploying a previous git SHA/image tag (CodeDeploy swaps traffic back with no downtime).

---

## 3) CI/CD (GitHub Actions)

Workflow: `.github/workflows/deploy-ecs.yml`

Jobs:

- `Deploy development` (auto on `develop` and `main`)
- `Deploy production (gated)` (only on `main`, or manual dispatch with `deploy_production=true`)

To gate production, configure a GitHub Environment:

1. Repo Settings -> Environments -> `production`
2. Add Required reviewers (human approval)
3. Add environment secrets (optional)

Required GitHub secrets (repo-level or environment-level):

- `AWS_ACCOUNT_ID`: AWS account number (for ECR login)
- `AWS_DEPLOY_ROLE_ARN`: IAM role to assume via OIDC
- `TF_STATE_BUCKET`: Terraform state bucket name
- `API_CERTIFICATE_ARN`: ACM cert ARN for ALB HTTPS (required in CI; production also uses it for the blue/green test listener)

Workflow usage:

- Run `Deploy (Development -> Production)` manually via GitHub Actions (`workflow_dispatch`)
- `deploy_development=true` deploys `environment=development`
- `deploy_production=true` deploys `environment=production` (only when run from `main`)

Deployment strategy:

- `development`: rolling ECS deployments (`enable_blue_green=false`).
- `production`: **CodeDeploy blue/green** (`enable_blue_green=true`) shifts ALB traffic between two target groups (automatic rollback on failure).
- The private ALB **test listener** (default `9000`, VPC-only) is **HTTPS** when `api_certificate_arn` is set.

---

## 4) Cost guidance (keep total < $500/mo)

Typical ballpark monthly costs per environment (region-dependent):

- ALB: ~$20-$30
- NAT Gateways: ~$32 each + data (2 AZs => $64+/env)
- ECS Fargate Spot (1-2 tasks, 0.5-1 vCPU, 1-2 GB): ~$15-$80
- ElastiCache Redis (small): ~$15-$60
- RDS (t4g.micro/small + storage) or Neon: ~$15-$150+
- Lambda + API Gateway (voice webhooks): usually <$5 unless high volume
- Secrets Manager: ~$0.40/secret/month + API calls

To stay under budget:

- Keep `development` at 1 task and the smallest Redis node.
- Terraform uses a single NAT gateway for non-production by default (production keeps one per AZ).
- Run the API service primarily on Spot with minimal on-demand base.

---

## 5) Parking (stop costs) + restore

To stop almost all ongoing AWS cost while keeping config for fast restore, destroy the expensive modules (VPC/NAT, ECS/ALB, RDS, Redis, voice gateway) and keep Secrets Manager secrets.

Development (example):

```bash
cd infra/terraform
terraform init -reconfigure \
  -backend-config="bucket=<bucket>" \
  -backend-config="key=medspa-ai-platform/development/terraform.tfstate" \
  -backend-config="region=us-east-1"

terraform destroy -auto-approve \
  -var="environment=development" \
  -target=module.ecs_fargate \
  -target=module.rds \
  -target=module.redis \
  -target=module.lambda \
  -target="module.api_gateway[0]" \
  -target=module.vpc
```

Production is the same with `key=medspa-ai-platform/production/terraform.tfstate` and `-var="environment=production"`.

Restore:

- Run the GitHub Actions workflow to recreate development when you're ready.
- Later, run it from `main` with `deploy_production=true` to recreate production.
