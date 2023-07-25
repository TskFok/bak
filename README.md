# 上传文件到百度云

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