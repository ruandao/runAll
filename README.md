# runAll

Multi-service orchestrator that reads a YAML config, starts services in correct DAG order, health-checks them, and serves a Web UI dashboard.

## Build

```bash
cd runAll
go build -o runAll .
```

Requires Go 1.24+.

## Usage

```bash
# Foreground mode with Web UI on :9999
./runAll --config ./config.yaml

# Start and exit (no Web UI)
./runAll --config ./config.yaml --daemon

# Custom UI port
./runAll --config ./config.yaml --ui-port :8080
```

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `config.yaml` | Path to YAML configuration file |
| `--daemon` | `false` | Start services and exit, no Web UI |
| `--ui-port` | `:9999` | Web UI listen address |

## YAML Configuration

### Full Example

```yaml
version: "1"
groups:
  - name: infrastructure
    services:
      - name: redis
        command: "redis-server --port 6379"
        health_check:
          url: "http://localhost:6379"
          timeout: 30
          retries: 10
          backoff:
            initial: 1.0
            max: 8.0
            multiplier: 2.0
        on_failure: exit

      - name: kafka
        command: "docker start kafka"
        health_check:
          url: "http://localhost:9092"

  - name: apps
    services:
      - name: saas-backend
        command: "python manage.py runserver 0.0.0.0:8000"
        health_check:
          url: "http://localhost:8000/api/health"
        depends_on: [redis, kafka]
        working_dir: ./task2app/Saas_project

      - name: go-relay
        command: "./go_relayToTrae"
        health_check:
          url: "http://localhost:9090/healthz"
        depends_on: [kafka]

      - name: frontend
        command: "npm run dev"
        health_check:
          url: "http://localhost:5173"
        depends_on: [saas-backend]
        on_failure: skip
```

### Field Reference

#### Service

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | **yes** | — | Unique service name across all groups |
| `command` | string | **yes** | — | Shell command to start the service (passed to `sh -c`) |
| `health_check` | object | **yes** | — | Health check configuration (see below) |
| `depends_on` | []string | no | `[]` | Service names this service depends on. Execution order is determined by `depends_on`, not by group order. |
| `on_failure` | string | no | `"exit"` | `exit` — stop all services and quit. `skip` — log error and continue. |
| `working_dir` | string | no | current dir | Working directory for the command. Relative paths are resolved against the YAML file's directory. |
| `env` | map | no | `{}` | Extra environment variables appended to the current environment. |

#### health_check

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `url` | string | **yes** | — | HTTP GET endpoint. 2xx/3xx = healthy. |
| `timeout` | int | no | `30` | Total timeout in seconds. |
| `retries` | int | no | `10` | Maximum number of retry attempts. |
| `backoff.initial` | float | no | `1.0` | First retry interval in seconds. |
| `backoff.max` | float | no | `8.0` | Maximum interval cap in seconds. |
| `backoff.multiplier` | float | no | `2.0` | Exponential multiplier per retry. |

#### groups

`groups` is for logical organization only. Execution order is determined solely by `depends_on`.

### DAG Execution

Services are sorted into execution levels using Kahn's topological sort:

```
redis ──┬── saas-backend ── frontend
kafka ──┤
        └── go-relay
```

- **Level 0:** `redis`, `kafka` (no dependencies → start in parallel)
- **Level 1:** `saas-backend` (waits for redis + kafka), `go-relay` (waits for kafka) → start in parallel
- **Level 2:** `frontend` (waits for saas-backend)

Within each level, all services start concurrently. The next level begins only after all services in the current level pass their health checks.

### Health Check Flow

```
interval = backoff.initial
for i in 0..retries:
    sleep(interval)
    GET url
    if 2xx/3xx → healthy
    interval = min(interval × multiplier, backoff.max)
    if elapsed > timeout → fail
```

## Web UI

When running in foreground mode, open `http://localhost:9999` (or the port specified by `--ui-port`).

**Features:**
- Service status dashboard with colored dots (green=healthy, yellow=starting, gray=pending, red=failed, dark=skipped)
- Each service row shows its dependencies and their current status
- Auto-refresh every 2 seconds
- **Restart button** (↻) on healthy/failed services — stops the process and re-launches with health check

## Shutdown

`Ctrl+C` or `SIGTERM` triggers graceful shutdown:
1. Services are stopped in **reverse DAG order** (dependents first, then their dependencies)
2. Each process receives `SIGTERM`
3. After 5 seconds, unresponsive processes receive `SIGKILL`
4. The entire process group is signaled, ensuring child processes also terminate

## Validation Rules

runAll validates the YAML config before starting:

- All service names must be unique across groups
- `depends_on` entries must reference existing service names
- No circular dependencies (e.g. `a → b → a`)
- `on_failure` must be `"exit"` or `"skip"`
- `command` and `health_check.url` are required for each service
