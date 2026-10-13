package cmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"bak/global"
	"bak/internal/backup"
	"bak/internal/server"
	"bak/internal/store"
)

const devFrontend = "http://127.0.0.1:5173"

type options struct {
	addr string
	dev  bool
}

// Execute 启动本地备份面板。程序不再接受 upload、flashToken 等子命令。
func Execute() {
	opt, err := parseArgs(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}
	if err := run(opt); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseArgs(args []string, errOut io.Writer) (options, error) {
	fs := flag.NewFlagSet("bak", flag.ContinueOnError)
	fs.SetOutput(errOut)
	addr := fs.String("addr", "127.0.0.1:8080", "监听地址，只允许回环地址")
	dev := fs.Bool("dev", false, "把非 /api 请求代理到 "+devFrontend)
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(errOut, "bak 是本地备份面板，不接受命令行子命令: %s\n", strings.Join(fs.Args(), " "))
		return options{}, fmt.Errorf("unexpected args")
	}
	return options{addr: *addr, dev: *dev}, nil
}

func run(opt options) error {
	if err := validateServeAddr(opt.addr); err != nil {
		return fmt.Errorf("监听地址无效: %w", err)
	}

	st := store.NewGorm(global.DataBase)
	if err := st.FailStaleRuns(context.Background()); err != nil {
		return fmt.Errorf("清理未完成的运行记录失败: %w", err)
	}

	api, schedule := server.NewMuxWithRunner(server.Deps{
		AuthDeps: server.AuthDeps{
			Users:      st,
			Sessions:   store.RedisSessions{},
			SessionTTL: time.Duration(global.JwtExpire) * time.Hour,
		},
		Store:     st,
		JWTSecret: global.JwtSecret,
		ChunkDir:  "runtime/chunks",
	})

	var frontend http.Handler
	if opt.dev {
		h, err := server.NewDevProxy(devFrontend)
		if err != nil {
			return fmt.Errorf("前端代理配置无效: %w", err)
		}
		frontend = h
	} else {
		frontend = server.NewStaticHandler()
	}

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for now := range ticker.C {
			if err := backup.Tick(context.Background(), st, schedule, now); err != nil {
				log.Printf("定时任务检查失败: %v", err)
			}
		}
	}()

	fmt.Printf("备份面板已启动: http://%s\n", opt.addr)
	if err := http.ListenAndServe(opt.addr, server.WithFrontend(api, frontend)); err != nil {
		return fmt.Errorf("服务退出: %w", err)
	}
	return nil
}

func validateServeAddr(addr string) error {
	if os.Getenv("BAIDUBAK_ALLOW_UNSPECIFIED_ADDR") == "1" {
		return server.ValidateContainerAddr(addr)
	}
	return server.ValidateAddr(addr)
}
