# 本地 Fork 镜像构建与外部 Deploy 目录启动

适用场景：

- 你的源码仓库在一个目录，例如 `/path/to/sub2api`
- 你的运行目录在另一个目录，例如 `/path/to/sub2api-deploy`
- 你希望继续在 `sub2api-deploy` 目录执行 `docker compose up -d --build`
- 同时镜像内容来自你本地 fork 的前后端代码，而不是远端 `weishaw/sub2api:latest`

## 原理

把 deploy 目录中的 `sub2api` 服务从：

```yaml
image: weishaw/sub2api:latest
```

改成：

```yaml
image: sub2api-local
build:
  context: /absolute/path/to/your/sub2api
  dockerfile: Dockerfile
```

这样做之后：

- 启动目录仍然是你的 deploy 目录
- `.env`、`data/config.yaml`、`postgres_data`、`redis_data` 继续复用
- `docker compose up -d --build` 会直接使用你本地 fork 源码构建镜像

`Dockerfile` 会同时构建前端和后端，所以不需要分别打包前后端。

## 一次性改造步骤

1. 备份 deploy 目录里的 `docker-compose.yml`
2. 修改 `sub2api` 服务，保留原有 `ports`、`volumes`、`environment`、`depends_on`
3. 只替换镜像来源为本地 `build`

示例：

```yaml
services:
  sub2api:
    image: sub2api-local
    build:
      context: /Users/yourname/path/to/sub2api
      dockerfile: Dockerfile
    container_name: sub2api
    restart: unless-stopped
    ports:
      - "${BIND_HOST:-0.0.0.0}:${SERVER_PORT:-8080}:8080"
    volumes:
      - ./data:/app/data
```

## 启动命令

进入 deploy 目录后执行：

```bash
docker compose up -d --build sub2api
```

如果想整套都带起来：

```bash
docker compose up -d --build
```

## 日常更新流程

每次本地 fork 有新代码后，重复执行：

```bash
cd /path/to/sub2api-deploy
docker compose up -d --build sub2api
```

这会：

- 重新读取 `/path/to/sub2api` 的最新代码
- 重新构建镜像
- 替换 `sub2api` 容器
- 保留 PostgreSQL、Redis 与已有数据目录

## 验证

```bash
docker compose ps
docker compose logs -f sub2api
```

## 不要这样做

当 `sub2api` 服务已经切换到本地源码构建后，不要再把“升级”理解为：

```bash
docker compose pull
```

原因：

- 你要运行的是本地 fork 构建结果
- `pull` 的语义是拉远端镜像，不是使用你本地代码

正确做法是：

```bash
docker compose up -d --build sub2api
```

## 数据与配置说明

以下内容会继续沿用，不需要重新初始化：

- `.env`
- `data/config.yaml`
- `postgres_data`
- `redis_data`

像“OpenAI注册机”这类新增后台模块，如果配置被设计为数据库配置项，也会保存在数据库 `settings` 表中，不需要额外再加宿主机配置文件。

## 回滚

如果你要回到远端官方镜像：

1. 恢复原来的 `docker-compose.yml`
2. 重新启动 `sub2api`

```bash
docker compose up -d sub2api
```

## 推荐命令摘要

```bash
cd /path/to/sub2api-deploy
docker compose up -d --build sub2api
docker compose logs -f sub2api
```
