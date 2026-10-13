package cmd

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"bak/global"
	"bak/internal/backup"
	"bak/internal/server"
	"bak/internal/store"

	"github.com/spf13/cobra"
)

const devFrontend = "http://127.0.0.1:5173"

var (
	serveAddr string
	serveDev  bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "启动本地备份面板",
	Long:  "在回环地址上启动备份面板，并每 30 秒检查一次定时任务。",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if err := validateServeAddr(serveAddr); err != nil {
			fmt.Fprintln(os.Stderr, "监听地址无效:", err)
			os.Exit(1)
		}

		st := store.NewGorm(global.DataBase)
		if err := st.FailStaleRuns(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "清理未完成的运行记录失败:", err)
			os.Exit(1)
		}

		api, run := server.NewMuxWithRunner(server.Deps{
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
		if serveDev {
			h, err := server.NewDevProxy(devFrontend)
			if err != nil {
				fmt.Fprintln(os.Stderr, "前端代理配置无效:", err)
				os.Exit(1)
			}
			frontend = h
		} else {
			frontend = server.NewStaticHandler()
		}

		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for now := range ticker.C {
				if err := backup.Tick(context.Background(), st, run, now); err != nil {
					log.Printf("定时任务检查失败: %v", err)
				}
			}
		}()

		fmt.Printf("备份面板已启动: http://%s\n", serveAddr)
		if err := http.ListenAndServe(serveAddr, server.WithFrontend(api, frontend)); err != nil {
			fmt.Fprintln(os.Stderr, "服务退出:", err)
			os.Exit(1)
		}
	},
}

func validateServeAddr(addr string) error {
	if os.Getenv("BAIDUBAK_ALLOW_UNSPECIFIED_ADDR") == "1" {
		return server.ValidateContainerAddr(addr)
	}
	return server.ValidateAddr(addr)
}

func init() {
	serveCmd.Flags().StringVar(&serveAddr, "addr", "127.0.0.1:8080", "监听地址，只允许回环地址")
	serveCmd.Flags().BoolVar(&serveDev, "dev", false, "把非 /api 请求代理到 "+devFrontend)
	rootCmd.AddCommand(serveCmd)
}
