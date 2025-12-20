# Bootstrap Deployment (Lightsail + Neon)

> Deprecated: Lightsail bootstrap is retained for reference only. New deployments should use ECS Fargate (Spot) + Lambda voice ingress + ElastiCache Redis: `docs/DEPLOYMENT_ECS.md`.

This guide covers the $200/mo bootstrap deployment that runs everything on a single AWS Lightsail instance while relying on managed Neon Postgres and a local Redis service.

## Architecture Summary

- **AWS Lightsail**: Ubuntu 22.04, 2 GB RAM, 1 vCPU. Hosts the Go API container with in-process workers and a local Redis service.
- **Neon PostgreSQL**: Serverless Postgres for multitenant data + job tables.
- **Redis**: Installed directly on the Lightsail host for conversation history + caching.
- **AWS Bedrock/Telnyx/Square**: Cloud APIs, configured via `.env`.
- **docker-compose.bootstrap.yml**: Builds and runs a single container with `USE_MEMORY_QUEUE=true`, so the API and workers share the same process/queue.

## 1. Prepare Managed Services

1. **Neon** – create a project and database, grab the connection string (e.g. `postgresql://user:pass@ep-xyz.neon.tech/medspa?sslmode=require`).
2. **AWS (Bedrock) / Telnyx / Square / Twilio** – provision API keys and AWS credentials you will paste into `.env`.
3. **Domain + DNS** – create DNS records in Cloudflare (or similar) for the API hostname.

## 2. Provision Lightsail

```bash
aws lightsail create-instances \
  --instance-names medspa-api \
  --availability-zone us-east-1a \
  --blueprint-id ubuntu_22_04 \
  --bundle-id medium_2_0
```

Then SSH in:

```bash
ssh ubuntu@<lightsail-ip>
```

Install prerequisites:

```bash
sudo apt update && sudo apt upgrade -y
sudo apt install -y docker.io docker-compose redis-server
sudo systemctl enable redis-server && sudo systemctl start redis-server
```

Tune Redis for 256 MB and LRU eviction:

```
echo "maxmemory 256mb" | sudo tee -a /etc/redis/redis.conf
echo "maxmemory-policy allkeys-lru" | sudo tee -a /etc/redis/redis.conf
sudo systemctl restart redis-server
```

## 3. Copy the Repo & Configure Environment

```bash
git clone https://github.com/wolfman30/medspa-ai-platform.git
cd medspa-ai-platform
cp .env.example .env
```

Edit `.env` with production values:

```
PORT=8080
ENV=production
LOG_LEVEL=info
USE_MEMORY_QUEUE=true
WORKER_COUNT=2
DATABASE_URL=postgresql://user:pass@ep-xyz.neon.tech/medspa?sslmode=require
REDIS_ADDR=localhost:6379
AWS_REGION=us-east-1
BEDROCK_MODEL_ID=anthropic.claude-3-haiku-20240307-v1:0
BEDROCK_EMBEDDING_MODEL_ID=amazon.titan-embed-text-v1
TELNYX_API_KEY=...
TELNYX_MESSAGING_PROFILE_ID=...
TELNYX_WEBHOOK_SECRET=...
SQUARE_ACCESS_TOKEN=...
SQUARE_LOCATION_ID=...
SQUARE_WEBHOOK_SIGNATURE_KEY=...
TWILIO_ACCOUNT_SID=...
TWILIO_AUTH_TOKEN=...
TWILIO_FROM_NUMBER=...
```

Neon already enforces TLS, so keep `sslmode=require` in the DSN.

## 4. Launch the Bootstrap Stack

`docker-compose.bootstrap.yml` is tailored for the bootstrap mode:

```bash
sudo docker compose -f docker-compose.bootstrap.yml up -d --build
```

- Runs the API container on the host network (so it can reach local Redis).
- Enables in-memory queue workers inside the same process.
- Restarts automatically if the instance reboots.

Check logs:

```bash
sudo docker compose -f docker-compose.bootstrap.yml logs -f api
```

## 5. Reverse Proxy (Optional but recommended)

Install Caddy for automatic HTTPS:

```bash
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/caddy-stable-archive-keyring.gpg] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main" | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update && sudo apt install -y caddy
```

`/etc/caddy/Caddyfile`:

```
api.yourdomain.com {
    reverse_proxy localhost:8080
}
```

Reload Caddy: `sudo systemctl reload caddy`.

## 6. Health Checks & Backups

- `curl http://localhost:8080/health` – verifies Postgres, Redis, Bedrock, Telnyx.
- Schedule Lightsail snapshots nightly while bootstrapping.
- Monitor Redis memory via `redis-cli INFO memory`.

## Upgrade Path

- Bump the Lightsail instance to 4 GB (bundle `large_2_0`) once CPU >80% or Redis evicts aggressively.
- When you hit ~20 clients, migrate to ECS/Fargate + RDS/ElastiCache and switch `USE_MEMORY_QUEUE` to `false` to reactivate SQS/Dynamo.
