package main

import (
	"BaiduYunPanBak/bootstrap"
	"BaiduYunPanBak/cmd"
)

func main() {
	bootstrap.Init()

	cmd.Execute()
}
