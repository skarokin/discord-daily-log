# discord-daily-log

private discord bot for thread analysis, tools tuned for nutrition notes

## setup

1. Create an application and bot in the [Discord Developer Portal](https://discord.com/developers/applications).
2. Enable the Message Content privileged intent.
3. Install it in server with the `applications.commands` and `bot` scopes.
4. Grant `View Channels`, `Read Message History`, `Send Messages`, `Send Messages in Threads`, `Attach Files`, and `Use Slash Commands` in the allowed log channel.
5. Copy the application ID, public key, bot token, guild ID, user ID, and the parent channel ID.
6. Register the guild commands:

```shell
python scripts/register_commands.py \
  --application-id "..." \
  --guild-id "..."
```

The script reads `DISCORD_BOT_TOKEN` from the environment or local `.env`. Guild commands update immediately, which is useful during development.

## local testing

Requirements:

- Go 1.25+
- Python 3.10+
- `cloudflared`
- Google Application Default Credentials
- A USDA FoodData Central API key

```shell
cp .env.example .env
gcloud auth application-default login
# fill .env, then:
python scripts/dev.py
```

The script runs the app and a Cloudflare quick tunnel. Copy the printed HTTPS URL into the Discord application's **Interactions Endpoint URL**, appending `/interactions`. Go and `.env` changes rebuild and restart only the local server, so the tunnel URL remains stable for the session.

Development mode uses an in-memory goal value and processes deferred work locally. It still validates real Discord signatures and calls Vertex AI, Discord, and USDA.

## deployment

The deployment uses OpenTofu in two small stages:

- `infra/foundation`: APIs, Artifact Registry, Firestore, Cloud Tasks, service accounts, IAM, and empty Secret Manager stores
- `infra/app`: one scale-to-zero Cloud Run service

Cloud Run must be publicly invokable so Discord can reach `/interactions`. That route requires a valid Discord Ed25519 signature. `/tasks/process` additionally requires the expected Cloud Tasks header and an OIDC token from the dedicated task identity.

### 1. configure foundation

```shell
cp infra/foundation/terraform.tfvars.example infra/foundation/terraform.tfvars
tofu -chdir=infra/foundation init
tofu -chdir=infra/foundation apply
```

### 2. add secret values manually

OpenTofu creates only the secret containers. It never creates, changes, or destroys secret versions.

Add enabled versions yourself in the Cloud Console or with `gcloud`:

```shell
gcloud secrets versions add discord-bot-token --data-file="path-to-token-file"
gcloud secrets versions add usda-api-key --data-file="path-to-usda-key-file"
```

### 3. configure and deploy the app

```shell
cp infra/app/terraform.tfvars.example infra/app/terraform.tfvars
# fill the Discord IDs, public key, and initial natural-language goal.
python scripts/deploy.py --project-id "your-project" --region "us-central1"
```
