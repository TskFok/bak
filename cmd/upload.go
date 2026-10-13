package cmd

import (
	"bak/internal/baidutoken"
	"bak/internal/provider/baidu"
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

// uploadCmd represents the upload command
var uploadCmd = &cobra.Command{
	Use:   "upload <本地文件绝对路径>",
	Short: "上传到百度云盘",
	Long:  "上传到百度云盘",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		localPath := args[0]
		if !filepath.IsAbs(localPath) {
			return fmt.Errorf("需要本地文件的绝对路径: %s", localPath)
		}

		sec, err := baidutoken.Current()
		if err != nil {
			return err
		}
		token := sec.AccessToken

		client := baidu.New(token, baidu.NewOpenAPIUploader(), filepath.Join("runtime", "chunks"), baidu.DefaultChunkSize)
		client.OnCreated = baidu.SaveHistory

		ctx := context.Background()
		if err := client.Test(ctx); err != nil {
			return err
		}
		//上传到的云盘地址 /bak/当前日期/文件名
		return client.Upload(ctx, localPath, baidu.RemotePath(localPath, time.Now()))
	},
}

func init() {
	rootCmd.AddCommand(uploadCmd)
}
