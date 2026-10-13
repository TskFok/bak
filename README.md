# bak

把本机目录备份到百度网盘、阿里云盘或 WebDAV 的本地面板。授权、任务和运行记录都在浏览器里完成，程序不提供上传或换票等命令行子命令。

## 能做什么

- 配置百度网盘、阿里云盘（应用凭据 + 授权码）和 WebDAV（地址、用户名、密码），并测试连接。
- 创建备份任务：本地目录、目标网盘、网盘目录、每天的备份时刻。
- 立即备份，或按设定时刻自动备份。未变化的文件会跳过。
- 查看每次运行的成功、跳过、失败数量，以及逐文件结果。

面板只监听本机。默认地址是 `127.0.0.1:8080`，`--addr` 也只接受回环地址。

## 准备

需要本机可访问的 MySQL 和 Redis。登录会话和网盘凭据用同一个密钥加密，启动前必须设置 `BAIDUBAK_JWT_SECRET`。密钥不要写入仓库。

内置配置在 `utils/conf/conf.yaml`。环境变量会覆盖对应项：

| 环境变量 | 作用 |
| --- | --- |
| `BAIDUBAK_MYSQL_DSN` | MySQL 连接串 |
| `BAIDUBAK_REDIS_HOST` | Redis 地址 |
| `BAIDUBAK_REDIS_USER` | Redis 用户名 |
| `BAIDUBAK_REDIS_PASSWORD` | Redis 密码 |
| `BAIDUBAK_JWT_SECRET` | 会话与凭据密钥 |
| `BAIDUBAK_CONFIG` | 额外 YAML 或 `.env` 的路径；未设置时，若存在 `/app/conf.yaml` 会合并进去 |

也可以把覆盖项写进 `BAIDUBAK_CONFIG` 指向的文件。进程环境变量优先于该文件。

## 启动

```
export BAIDUBAK_JWT_SECRET='请换成足够长的随机字符串'
go run .
```

浏览器打开 http://127.0.0.1:8080 。第一次打开会要求创建本地账号，账号只存在这台电脑上。之后用该账号登录。

可选参数：

```
go run . --addr 127.0.0.1:9090
go run . --dev
```

`--dev` 把页面请求转发到 `http://127.0.0.1:5173`，`/api` 仍由 Go 处理。

传入 `serve`、`upload`、`flashToken`、`reflashToken` 等子命令会直接退出。网盘授权和备份都在面板里操作。

## 开发前端

前端在 `web` 目录，使用 Vite + React。开发时另开一个终端：

```
cd web && npm install && npm run dev
```

再用 `go run . --dev` 启动 Go 服务。

发布前构建并复制到嵌入目录：

```
cd web && npm run build && rm -rf ../internal/server/dist/assets && cp -R dist/. ../internal/server/dist/
```

## Docker

镜像按目标架构构建：

```
make docker-build-linux-amd64
make docker-build-linux-arm64
```

`docker-compose.yml` 使用 `bak:linux-amd64`。面板通过端口映射到宿主机 `127.0.0.1`，容器内监听 `0.0.0.0:8080`（需要 `BAIDUBAK_ALLOW_UNSPECIFIED_ADDR=1`，compose 已经设置）。改宿主机端口时设置 `BAIDUBAK_PORT`。

默认通过 `host.docker.internal` 连接宿主机上的 MySQL 和 Redis。这些服务若只监听 `127.0.0.1`，容器访问不到。没有现成数据库时，用 compose profile 启动自带的 MySQL / Redis，并把连接改成服务名：

```
# .env
BAIDUBAK_MYSQL_DSN=root:@tcp(mysql:3306)/yy?charset=utf8mb4&parseTime=True&loc=Local
BAIDUBAK_REDIS_HOST=redis:6379
BAIDUBAK_JWT_SECRET=请换成足够长的随机字符串
```

```
docker compose --profile mysql --profile redis up -d
```

只缺一侧时，只加对应的 `--profile mysql` 或 `--profile redis`。`.env.example` 里有可复制的变量说明。自带 MySQL、Redis 映射到宿主机的端口由 `BAIDUBAK_MYSQL_PORT`、`BAIDUBAK_REDIS_PORT` 控制，默认 3306 和 6379，只绑定 `127.0.0.1`。
