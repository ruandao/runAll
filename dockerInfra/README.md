# dockerInfra — 本地开发基础设施

Redis 与 Kafka 拆分为两个独立 Docker Compose 栈，由 runAll 分别编排为 `docker-redis` 与 `docker-kafka`。

## 目录

| 路径 | 内容 | 端口 |
|------|------|------|
| `redis/` | Redis 7 | `6379`（`port_config.json` → `dockerInfra.redisPort`） |
| `kafka/` | Zookeeper + Kafka + Kafka UI | `9093`、`18080`（`dockerInfra.kafkaBootstrapServers` / `kafkaUiPort`） |

## 手工命令

```bash
bash runAll/dockerInfra/redis/run.sh start    # 或 managed / stop
bash dockerInfra/kafka/run.sh start
```

## runAll

```yaml
docker-redis: working_dir dockerInfra/redis, health_check.tcp 127.0.0.1:6379
docker-kafka: working_dir dockerInfra/kafka, health_check.url http://127.0.0.1:18080
```

默认开发（`domainEvents.transport=redis`）只需启动 `docker-redis`。Kafka transport 或 AiProvider 镜像 hash 等场景需额外启动 `docker-kafka`。

## 迁移说明

原 `task2app/Saas_project/docker-compose.yml` 与 `run-infra.sh` 已移除，请改用本目录。
