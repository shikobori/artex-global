# Reuse the local Codex ChatGPT login

ARTEX's **Codex (ChatGPT login)** LLM format reads the existing Codex CLI
credential cache. It does not add a separate OAuth flow, copy the tokens into
PostgreSQL, or send them to the browser.

## Setup

1. Install `codex` on the ARTEX server's `PATH`.
2. Run `codex login` as the same OS user that runs ARTEX, choosing ChatGPT sign-in.
   ARTEX reads `$CODEX_HOME/auth.json` when `CODEX_HOME` is set; otherwise it reads
   `~/.codex/auth.json` in that OS user's home directory.
3. In **System → LLM settings**, add a profile and select **Codex (ChatGPT login)**.
4. Load the model list, select a model, test the connection, save, and activate
   the profile (or assign it to a task).

The login status indicator checks the cached credential shape. The connection
test verifies that the selected model actually accepts a request. Authentication
must exist on the server, not just on the computer running the browser.

For an environment-configured provider, use:

```sh
export ARTEX_LLM_PROVIDER=codex
export ARTEX_LLM_MODEL='<model from your Codex model list>'
# Optional: a non-default Codex configuration directory
# export CODEX_HOME=/absolute/path/to/codex-home
```

No `OPENAI_API_KEY` is required. Existing OpenAI/Anthropic formats keep their
usual API-key authentication. Codex API-key logins are deliberately not treated
as ChatGPT subscription logins.

If Codex stores credentials only in the OS keychain, this file-based integration
will report that no usable cache exists. Use Codex's file credential store if
appropriate for your environment:

```sh
codex -c 'cli_auth_credentials_store="file"' login
```

## Docker Compose

The standard `docker-compose.yml` accepts the full contents of `auth.json` in
`ARTEX_CODEX_AUTH_JSON` in `.env`. No extra Compose file or credential mount is
needed. The image includes the Codex CLI and imports the JSON at startup into
`/app/data/codex/auth.json`, persisted by the existing `./data` volume.

### Build the local image

Compose uses `artex:local` with pulling disabled for that image. Its `local` build
target compiles the frontend and Go binary inside Docker. No host Go/npm tools
or `dist/amd64/artex` file are required. The default platform is `linux/amd64`;
for ARM64, change `platform` in `docker-compose.yml` to `linux/arm64`.

```sh
# Requires Docker and Docker Compose.
docker compose build artex
```

After configuring `.env` below, run `docker compose up -d --build` to build and
start the services. Repeat that command after source changes. The separate
`release` Dockerfile target still accepts precompiled binaries for release CI.

### Paste the existing login into `.env`

Copy `.env.example` to `.env` if needed, set `POSTGRES_PASSWORD`, and paste the
**complete contents** of your Codex `auth.json` between single quotes:

```dotenv
ARTEX_LLM_PROVIDER=codex
ARTEX_LLM_MODEL=your-codex-model-id
ARTEX_CODEX_AUTH_JSON='{
  "auth_mode": "chatgpt",
  "OPENAI_API_KEY": null,
  "tokens": {
    "id_token": "paste the actual value",
    "access_token": "paste the actual value",
    "refresh_token": "paste the actual value",
    "account_id": "paste the actual value"
  },
  "last_refresh": "paste the actual value"
}'
```

The example only shows the layout; paste your actual file, including all of its
fields. Single quotes preserve the JSON literally, and Compose supports
multiline values. Escape a literal single quote inside the value as `\'` if your
file contains one. Do not use `docker run --env-file` for this multiline format.

```sh
chmod 600 .env
docker compose up -d
```

If you prefer to choose the model in the UI, leave `ARTEX_LLM_MODEL` blank, then
select **Codex (ChatGPT login)**, load the model list, test, save, and activate
it in ARTEX's LLM settings. Existing database profiles take precedence over
environment variables. When JSON is supplied and no provider is explicitly set,
the container defaults to Codex.

On startup the importer validates the JSON, writes credentials atomically with
mode `0600`, and removes `ARTEX_CODEX_AUTH_JSON` from the environment passed to
ARTEX and its child tools. Malformed or incomplete input stops startup without
printing the supplied value. The `./data/codex` directory has mode `0700`.

A fingerprint of the imported JSON prevents the original `.env` value from
overwriting refreshed tokens on subsequent starts. Changing formatting alone
also preserves the refreshed login. To replace the login, paste a fresh
`auth.json` into `.env` and run `docker compose up -d --force-recreate artex`.
Keep `./data/codex` across deployments. Removing the `.env` value does not erase
an existing login; use `docker compose exec artex codex logout` to sign out.

The `.env` file and Docker's configured container environment contain secrets;
Docker administrators and commands such as `docker compose config` can see them.
Do not publish their output. `.env` and credential files are excluded from Git
and Docker build contexts. The same-user file access limitation below still
applies inside the container.

## Authentication and protocol

- ARTEX rereads the cache before each request, so CLI login changes are picked up.
- Near expiry, or once after an HTTP 401, ARTEX runs `codex app-server` over stdio
  and requests `account/read` with `refreshToken: true`. Codex performs token
  rotation and writes the cache; the LLM provider does not use the refresh token.
- Refreshes within ARTEX are serialized. A separate Codex process can still
  update the same cache; refresh failures are surfaced rather than overwritten.
- The model picker uses Codex's `model/list` RPC. Neither RPC starts an agent turn.
- Inference uses the fixed Codex ChatGPT Responses endpoint. Profile URLs,
  proxies, and custom authentication headers cannot redirect these credentials.
  Redirects are rejected. The provider always streams with `store: false`, strips
  unsupported sampling/output-limit parameters, and requires a completed stream.
- ARTEX continues to run its own tool loop. Calls such as compaction that need a
  complete response accumulate the stream internally.

This adapter depends on Codex's credential-file and inference protocol, which
can change with CLI/backend updates. It is distinct from the public OpenAI API
and from the separate Sign in with ChatGPT integration. Use the ordinary OpenAI
provider for Platform API keys.

## Security boundary

Tokens are not stored in ARTEX profiles or returned in API responses. The Docker
startup importer removes the JSON seed before launching ARTEX and its child
processes. Inference attaches the access token only to the fixed HTTPS
destination. Token rotation is delegated to the Codex CLI; ARTEX does not
implement its own refresh-token exchange.

**This is not credential isolation from ARTEX tools.** Shell/file tools running
as the same OS user can read `$CODEX_HOME/auth.json` (by default
`~/.codex/auth.json` natively, or `/app/data/codex/auth.json` in Docker), just as
ARTEX can. File mode
`0600` does not protect it from that user. Use only in a trusted, isolated runtime;
separating credentials from arbitrary tool execution requires a separate OS
identity or credential service. The implementation does not create that boundary.

Official references:

- [Codex authentication](https://learn.chatgpt.com/docs/auth)
- [App-server authentication and model catalog](https://learn.chatgpt.com/docs/app-server)
