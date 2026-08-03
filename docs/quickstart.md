# 10-minute quickstart

This guide gets a local ai-cloud-ops instance running and analyzes one Alibaba Cloud account.

## 1. Prerequisites

Install or prepare:

- Python 3.11 or newer
- [uv](https://docs.astral.sh/uv/getting-started/installation/)
- Docker with Docker Compose
- An Alibaba Cloud account with permission to create RAM roles and CloudMonitor subscriptions

Confirm the tools are available:

```bash
python3 --version
uv --version
docker compose version
```

## 2. Install

Clone the repository, enter it, and install the project with development dependencies:

```bash
uv sync
```

To install into an existing uv-managed environment instead:

```bash
uv pip install -e .
```

## 3. Configure

Create your local environment file and protect it before adding secrets:

```bash
cp .env.example .env
chmod 0600 .env
```

Edit `.env` and set at least:

```dotenv
ALIYUN_ACCESS_KEY_ID=your-access-key-id
ALIYUN_ACCESS_KEY_SECRET=your-access-key-secret
ALIYUN_ACCOUNTS={"prod":"acs:ram::1234567890123456:role/ai-cloud-ops"}
ALIYUN_REGIONS={"prod":["cn-hangzhou"]}
POSTGRES_PASSWORD=choose-a-strong-local-password
```

Never commit `.env`. Prefer a dedicated RAM user or workload identity over an account owner AccessKey.

## 4. Set up the RAM role

Create a RAM role in each managed account. See Alibaba Cloud's
[RAM role documentation](https://www.alibabacloud.com/help/en/ram/user-guide/ram-role-overview).

1. Create a role named `ai-cloud-ops` (or choose your own name).
2. Attach only the read permissions needed for CloudMonitor and the resources you analyze.
3. Add the calling RAM user or workload identity to the role's trust policy.
4. Put the resulting Role ARN in `ALIYUN_ACCOUNTS`.

The trust policy answers **who may assume the role**. Resource policies answer **what the assumed role may do**. Keep both narrow: trust only the intended principal, and avoid broad administrator policies.

## 5. Set up the CloudMonitor webhook

In the Alibaba Cloud console, open **CloudMonitor → Event Center → Event Subscription**, then create or edit a subscription:

1. Select the products, event types, and severity levels to ingest.
2. Add an HTTP callback contact that points to your public webhook endpoint, for example `https://ops.example.com/webhooks/cloudmonitor`.
3. Set the same signing secret in CloudMonitor and `WEBHOOK_SIGNING_SECRET` in `.env`.
4. Send a test event and confirm the callback returns a successful HTTP status.

For local testing, use a secure HTTPS tunnel to port `8080`; CloudMonitor cannot call `localhost`. See the
[CloudMonitor Event Subscription documentation](https://www.alibabacloud.com/help/en/cloudmonitor/user-guide/create-an-event-subscription-policy).

## 6. Run

Start the local services:

```bash
docker compose up -d
```

Run an analysis for the configured account and region:

```bash
uv run ai-cloud-ops analyze --account prod --region cn-hangzhou
```

Follow service logs if startup takes a moment:

```bash
docker compose logs -f backend
```

## 7. Verify

Check service health and Prometheus metrics:

```bash
curl --fail http://localhost:8081/healthz
curl --fail http://localhost:8080/metrics
```

A healthy setup returns a successful status from `/healthz` and Prometheus text from `/metrics`.

## 8. Troubleshooting

### `POSTGRES_PASSWORD required in .env`

Set a non-empty `POSTGRES_PASSWORD` in `.env`, then rerun `docker compose up -d`.

### `InvalidAccessKeyId.NotFound` or `SignatureDoesNotMatch`

Recheck `ALIYUN_ACCESS_KEY_ID` and `ALIYUN_ACCESS_KEY_SECRET`. Remove accidental quotes or whitespace, and verify the AccessKey is active.

### `NoPermission` or `Forbidden.RAM`

Confirm the caller is listed in the role trust policy and may call `sts:AssumeRole`. Confirm the role itself has read permissions for the requested CloudMonitor and cloud resource APIs.

### Account or region not found

The CLI values must exactly match keys in `ALIYUN_ACCOUNTS` and regions in `ALIYUN_REGIONS`. For this guide, use `prod` and `cn-hangzhou`.

### Webhook events do not arrive

Confirm the callback uses public HTTPS, the tunnel or reverse proxy forwards to port `8080`, and `WEBHOOK_SIGNING_SECRET` matches. Check `docker compose logs backend` for signature or routing errors.

### Health check fails

Run `docker compose ps` and `docker compose logs backend postgres redis`. Port conflicts are common; stop the conflicting process or change the exposed ports in `.env`.

### Start cleanly after a bad local setup

```bash
docker compose down
docker compose up -d
```

Add `-v` to `docker compose down` only if you intentionally want to delete the local database volume.
