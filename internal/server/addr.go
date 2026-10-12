package server

import (
	"fmt"
	"net"
	"strings"
)

func ValidateAddr(addr string) error {
	return checkAddr(addr, false)
}

// ValidateContainerAddr 允许回环地址，以及 0.0.0.0 / ::。
// 容器内必须监听未指定地址，端口映射才能把流量转到进程。
func ValidateContainerAddr(addr string) error {
	return checkAddr(addr, true)
}

func checkAddr(addr string, allowUnspecified bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	if allowUnspecified && ip != nil && ip.IsUnspecified() {
		return nil
	}
	if allowUnspecified {
		return fmt.Errorf("只允许监听回环地址或未指定地址")
	}
	return fmt.Errorf("只允许监听回环地址")
}
