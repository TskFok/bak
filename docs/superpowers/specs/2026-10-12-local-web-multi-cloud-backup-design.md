# 本机多云盘备份面板设计

日期：2026-10-12

## 目标

提供只监听本机的 Web 面板。用户用一个本地账号登录后，把本地目录单向备份到百度网盘、阿里云盘或 WebDAV。备份可以手动立即执行，也可以每天在固定时刻自动执行。直接运行程序即启动面板，不提供命令行子命令。

成功标准：

- 第一次打开面板时创建唯一本地账号，之后必须登录才能看到任务和网盘。
- 能分别保存并测试三种网盘连接。
- 能创建备份任务，立即运行，并按每天的时刻自动运行。
- 未变化的文件跳过；单个文件失败不中断其余文件。
- 运行记录能看出成功、部分成功或失败，以及每个文件的结果。
- 直接运行程序启动面板。传入 `upload`、`flashToken`、`reflashToken`、`serve` 等子命令时拒绝启动。

## 已确认的范围

- 产品形态：本机 Web 面板，单用户自用。
- 第一版网盘：百度网盘、阿里云盘、WebDAV。
- 同步方式：本地目录单向上传到一个网盘目录。云端变化不回写本地，也不因为本地删除而去删除云端文件。
- 账号：首次设置一个用户名和密码。不支持多个面板账号。
- 触发：立即备份，加上每天一次的定时。
- 实现形态：同一个 Go 进程提供 HTTP 服务和定时器。React 页面构建后嵌进该进程。

不在第一版：

- 双向同步、冲突处理、云端删除。
- 多用户、注册邀请、对外网监听。
- 夸克、天翼、OneDrive、Google Drive。
- 失败文件自动重试。再次运行时，已成功且未变化的文件会跳过。
- 桌面安装包。
- 命令行上传、换票，以及 `serve` 这类子命令。

## 架构

直接运行程序启动面板，默认监听 `127.0.0.1:8080`。可用 `--addr` 修改地址，但只接受回环地址。`--dev` 时把页面请求代理到 `http://127.0.0.1:5173`，API 仍由 Go 处理。未加 `--dev` 时，Go 用 `go:embed` 提供 `web/dist`。不要增加 cobra 子命令。

继续使用现有 MySQL 和 Redis。配置仍来自 `utils/conf/conf.yaml`。路由使用 chi，保持 Go 1.20，不因为配置注释里的 Gin 字样引入 Gin。

分层：

| 单元 | 职责 | 依赖 |
| --- | --- | --- |
| `web/` | 登录、设置、概览、网盘、任务、运行记录 | 只调用 `/api` |
| `server` | HTTP、会话校验、参数校验 | auth、backup、provider |
| `auth` | 创建唯一用户、校验密码、读写 Redis 会话 | MySQL、Redis |
| `provider` | 检查连接、确保目录、上传一个文件 | 各网盘实现 |
| `backup` | 扫描目录、跳过未变化文件、写运行记录 | provider、MySQL |
| `schedule` | 判断今天是否到了任务时刻，并调用 backup | backup |

百度分片上传在百度适配器中完成。程序不解析上传路径，也不提供 `upload` 命令。

### 网盘接口

```go
type Provider interface {
    Test(ctx context.Context) error
    EnsureDir(ctx context.Context, remoteDir string) error
    Upload(ctx context.Context, localPath, remotePath string) error
}
```

- 百度：复用 `tool/openapi`。授权保持当前 out-of-band 方式：页面给出授权链接，用户把 code 粘贴回来，服务端换 access token 和 refresh token。上传仍是预创建、约 4MB 分片、创建文件。分片写入 `runtime/chunks/`，上传结束后删除。不再把整个文件的分片路径交给用户指定。文件大小使用 int64，避免现有 `UploadHistory.Size` 的 int32 上限。
- 阿里云盘：用户填写自己的 client id 和 client secret。授权和换 token 使用阿里云盘开放平台 `https://openapi.alipan.com` 的 OAuth 授权码流程。上传使用同一开放平台的文件上传接口。连接或换 token 失败时，把开放平台返回的错误显示在「测试连接」结果中，不改用非官方私有接口。
- WebDAV：保存服务器地址、用户名、密码。`PROPFIND` 测试连接并确认目录，不存在则 `MKCOL`，上传使用 `PUT`。

密钥字段用 AES-GCM 加密后入库。密钥材料是配置项 `jwt.secret` 的 SHA-256。日志、接口响应和错误文本不包含密码、client secret、access token 或 refresh token。

## 页面

服务只对本机开放。未登录只能访问设置页和登录页。

1. **首次设置**：用户表为空时显示。字段为用户名、密码、确认密码。密码至少 8 位。创建成功后写入会话并进入概览。已有用户时，设置接口拒绝再次创建。
2. **登录**：用户名或密码错误时，在表单旁显示「用户名或密码错误」。成功后写入会话。
3. **概览**：三种网盘是否已连接、任务数量、最近运行的状态。
4. **网盘**：分别配置百度、阿里云盘、WebDAV，每条可测试连接。
5. **备份任务**：名称、本地目录、目标网盘、网盘目录、每天时刻（本地时区 `HH:MM`）、是否启用。列表可立即备份、暂停定时、删除。一条任务只指向一个网盘目录。同一本地目录要备份到多个网盘时，建立多条任务。
6. **运行记录**：开始时间、结束时间、状态、成功数、跳过数、失败数、错误摘要。详情列出每个文件的相对路径和结果。

左侧导航：概览、网盘、备份任务、运行记录。底部退出登录。

界面要求：浅色内容区配深色侧栏；正文至少 16px；表单有可见标签；状态同时用文字表示（成功、部分成功、失败、进行中、跳过），不只靠颜色；图标使用 SVG；交互元素可键盘聚焦。宽度从 1280px 到 768px 可用，不出现整页横向滚动。

## 数据

新表由 GORM AutoMigrate 创建。已有 `tokens`、`upload_histories` 保留。跳过判断以新的文件结果表为准，不以 `upload_histories` 为准。百度适配器仍可写入 `upload_histories`，作为百度文件号的记录。

### users

- `id`
- `username`：唯一，1 到 32 个字符
- `password_hash`：bcrypt
- `created_at`、`updated_at`

全表只允许一行。设置接口在事务中统计行数，已有一行时拒绝创建。

### cloud_accounts

- `id`
- `provider`：`baidu`、`aliyun`、`webdav` 之一，唯一
- `display_name`
- `secret_blob`：加密后的 JSON
- `status`：`connected` 或 `disconnected`
- `last_error`：最近一次测试或授权的可展示错误，不含密钥
- `created_at`、`updated_at`

百度 JSON 字段：`client_id`、`client_secret`、`access_token`、`refresh_token`、`expires_at`。阿里云盘字段相同。WebDAV 字段：`url`、`username`、`password`。

面板保存或刷新百度令牌时，同时更新 `tokens` 和 Redis 的 `access_token`，供面板与令牌表对齐。命令行不再读写这份令牌。

### backup_tasks

- `id`
- `name`
- `local_dir`：绝对路径
- `cloud_account_id`
- `remote_dir`：以 `/` 开头的网盘目录
- `schedule_time`：`HH:MM`，本地时区
- `enabled`：是否参与定时
- `last_scheduled_on`：最近一次定时触发的日期，可空
- `created_at`、`updated_at`

### backup_runs

- `id`
- `task_id`
- `trigger`：`manual` 或 `schedule`
- `status`：`running`、`success`、`partial`、`failed`
- `started_at`、`finished_at`
- `success_count`、`skip_count`、`fail_count`
- `error_summary`：整次失败原因，可空

### backup_file_results

- `id`
- `run_id`
- `task_id`
- `relative_path`
- `size`：int64
- `mod_time`：文件修改时间
- `status`：`success`、`skipped`、`failed`
- `error`：可空
- `source_fingerprint`：本地目录、网盘账号 ID、远程目录三者的哈希

跳过规则：同一任务下，该相对路径最近一条 `success` 记录的 `size`、`mod_time` 和 `source_fingerprint` 与当前文件及当前任务配置一致，则本次记为 `skipped`，不上传。任务的本地目录、网盘或远程目录一旦改变，指纹不再匹配，文件会重新上传。

## 会话

登录成功后生成随机会话 ID，写入 Redis，键为 `session:<id>`，值为用户 ID。TTL 使用配置项 `jwt.expire` 的小时数（当前为 3）。浏览器 Cookie 名为 `session`，HttpOnly、SameSite=Lax，Path=/。因为只监听 HTTP 回环地址，不设置 Secure。退出登录删除 Redis 键和 Cookie。

除 `POST /api/setup`、`GET /api/setup/status`、`POST /api/login` 和静态页面外，其余 `/api` 请求没有有效会话时返回 401。

## HTTP 接口

- `GET /api/setup/status` → `{ "needsSetup": true|false }`
- `POST /api/setup` → 用户名、密码、确认密码
- `POST /api/login`、`POST /api/logout`、`GET /api/me`
- `GET /api/overview`
- `GET /api/clouds`、`PUT /api/clouds/:provider`、`POST /api/clouds/:provider/test`、`POST /api/clouds/:provider/authorize`
- `GET /api/tasks`、`POST /api/tasks`、`PUT /api/tasks/:id`、`DELETE /api/tasks/:id`、`POST /api/tasks/:id/run`
- `GET /api/runs`、`GET /api/runs/:id`

`POST /api/tasks/:id/run` 在任务已有 `running` 记录时返回 409。

## 一次备份

手动运行和定时运行进入同一个运行器。

1. 若该任务已有 `running` 记录，拒绝新的触发。手动请求返回 409；定时触发只跳过这一轮。
2. 插入 `backup_runs`，状态为 `running`。
3. 读取任务和网盘账号并解密。令牌将在 5 分钟内到期时先刷新。刷新失败则整次记为 `failed`。
4. 本地目录不存在或不可读时，整次记为 `failed`。
5. 递归扫描文件。目录项不上传。用相对路径、大小、修改时间和 `source_fingerprint` 决定跳过或上传。
6. 调用 `EnsureDir`，再对每个待上传文件调用 `Upload`。远程路径是任务的 `remote_dir` 加上相对路径。
7. 单个文件失败时写入 `failed` 和错误文本，继续下一个文件。
8. 结束后：失败数为 0 时为 `success`；成功数和跳过数都为 0 且失败数大于 0 时为 `failed`；其余有失败的情况为 `partial`。没有任何文件时为 `success`，计数都是 0。

定时器每 30 秒检查一次。任务同时满足以下条件才触发：`enabled` 为真、当前本地时间已经达到 `schedule_time`、`last_scheduled_on` 不是今天、该任务没有 `running` 记录。通过检查后，先把 `last_scheduled_on` 更新为今天，再开始运行，避免慢任务在同一天重复进入。若当时已有 `running` 记录，则不更新 `last_scheduled_on`，本轮跳过，等进行中的运行结束后仍可在今天触发。手动运行不修改 `last_scheduled_on`。

进程启动时，把遗留的 `running` 记录改为 `failed`，`error_summary` 为「进程已退出」。

概览和运行详情每 3 秒重新请求进行中的记录。

## 启动

直接运行程序启动面板和定时器。可选参数只有：

- `--addr`：默认 `127.0.0.1:8080`，只接受回环地址。
- `--dev`：把非 `/api` 请求代理到 `http://127.0.0.1:5173`。

位置参数（包括 `serve`、`upload`、`flashToken`、`reflashToken`）使进程拒绝启动。网盘授权和备份都在面板里完成。

## 错误表现

- 设置页：密码不一致、密码短于 8 位、用户名为空，显示在对应字段旁。
- 登录页：统一显示「用户名或密码错误」。
- 网盘页：密钥缺失、授权码无效、WebDAV 无法访问或认证失败，显示在该账号的测试结果中。
- 任务页：本地目录无效时，保存任务即返回字段错误；运行中的整次失败写入运行记录。
- 重复点击立即备份：提示该任务正在备份。

## 测试

单元测试不访问真实网盘。

- 设置只能成功一次。密码错误不能登录。无会话访问任务接口返回 401。
- 假适配器：未变化文件跳过；单文件失败不中断其他文件；状态分别为 success、partial、failed；同一任务的第二次运行在第一次未结束时被拒绝。
- WebDAV 适配器使用 `httptest`：认证失败、创建目录、PUT 成功。
- 定时判断：时刻未到不触发；今天已经触发过不再触发；手动运行不改变今天的定时资格。
- 启动清理：遗留 `running` 记录变为 `failed`。

浏览器验收：首次设置、登录、保存一个 WebDAV、创建任务、立即备份、在运行记录中看到文件结果。百度和阿里云盘的真实授权在本机手工验证。

## 实现时保持的行为边界

- 监听地址不是回环地址时，进程拒绝启动。容器内设置 `BAIDUBAK_ALLOW_UNSPECIFIED_ADDR=1` 后才允许 `0.0.0.0`。
- 删除任务不删除运行记录和文件结果，避免丢掉跳过判断所需的历史。若任务已删除，历史记录仍可查看，但不能再运行。
- 跳过判断使用 `backup_file_results.source_fingerprint`。任务的本地目录、网盘账号或远程目录变化后，新运行的指纹与旧成功记录不同，文件会重新上传。
