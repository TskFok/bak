package main

import (
	"bak/bootstrap"
	"bak/cmd"
)

func main() {
	bootstrap.Init()

	cmd.Execute()
}
