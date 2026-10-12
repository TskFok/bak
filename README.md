# 上传文件到百度云

## 本地备份面板

``````
go run main.go serve
``````

浏览器打开 http://127.0.0.1:8080

首次打开设置本地账号，之后用该账号登录。面板里可以配置网盘（百度网盘、阿里云盘、WebDAV）、创建备份任务、查看运行记录。

命令行上传仍然可用：

``````
go run main.go upload 文件绝对路径
``````

### 开发前端

前端在 `web` 目录，使用 Vite + React。开发时另开一个终端：

``````
cd web && npm install && npm run dev
``````

再用 `go run main.go serve --dev` 启动 Go 服务，它会把页面请求转发给 `http://127.0.0.1:5173`，`/api` 仍由 Go 处理。

发布前构建并复制到嵌入目录：

``````
cd web && npm run build && rm -rf ../internal/server/dist/assets && cp -R dist/. ../internal/server/dist/
``````

## 旧的命令行用法

``````
获取code
https://openapi.baidu.com/oauth/2.0/authorize?response_type=code&client_id=你的appkey&redirect_uri=oob&scope=basic,netdisk&device_id=你的appid
获取token
go run main.go flashToken 获取到的code
刷新token
go run main.go reflashToken
上传文件
go run main.go upload 文件绝对路径 临时切片文件保存路径
``````
