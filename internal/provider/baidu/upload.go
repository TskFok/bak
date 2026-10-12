// Package baidu 把百度网盘的分片上传封装为 provider.Provider。
package baidu

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"

	"BaiduYunPanBak/model"
)

// DefaultChunkSize 是百度网盘分片上传的默认分片大小（4MB）。
const DefaultChunkSize = 4 * 1024 * 1024

// Uploader 抽象了百度开放接口中与上传相关的调用，便于测试替换。
type Uploader interface {
	Precreate(ctx context.Context, token, remote string, size int64, blocks []string) (string, error)
	UploadPart(ctx context.Context, token, remote, uploadID string, seq int, chunkPath string) error
	Create(ctx context.Context, token, remote, uploadID string, size int64, blocks []string) (fsID, md5 string, errno int, err error)
	Mkdir(ctx context.Context, token, remote string) error
	// Probe 用一次只读调用确认 token 可用。
	Probe(ctx context.Context, token string) error
}

// Client 实现 provider.Provider。
type Client struct {
	token     string
	up        Uploader
	chunkDir  string
	chunkSize int

	// OnCreated 在合并文件（create）返回后调用，用于记录上传历史，可为空。
	OnCreated func(size int64, remote, fsID, md5 string, errno int)
}

func New(token string, up Uploader, chunkDir string, chunkSize int) *Client {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	return &Client{token: token, up: up, chunkDir: chunkDir, chunkSize: chunkSize}
}

func (c *Client) Test(ctx context.Context) error {
	if c.token == "" {
		return fmt.Errorf("未连接")
	}
	return c.up.Probe(ctx, c.token)
}

func (c *Client) EnsureDir(ctx context.Context, remoteDir string) error {
	return c.up.Mkdir(ctx, c.token, remoteDir)
}

// Upload 按 chunkSize 流式切片写入 chunkDir，上传完成（或失败）后删除分片。
func (c *Client) Upload(ctx context.Context, localPath, remotePath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.chunkDir, 0o755); err != nil {
		return err
	}
	// 每次上传使用独立子目录，避免并发上传时分片互相覆盖。
	workDir, err := os.MkdirTemp(c.chunkDir, "upload-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	buf := make([]byte, c.chunkSize)
	var blocks []string
	var parts []string
	for seq := 0; ; seq++ {
		n, readErr := io.ReadFull(f, buf)
		if readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			return readErr
		}
		chunkPath := filepath.Join(workDir, fmt.Sprintf("chunk%d.dat", seq))
		if err := os.WriteFile(chunkPath, buf[:n], 0o644); err != nil {
			return err
		}
		parts = append(parts, chunkPath)
		sum := md5.Sum(buf[:n])
		blocks = append(blocks, hex.EncodeToString(sum[:]))
		if readErr == io.ErrUnexpectedEOF {
			break
		}
	}

	uploadID, err := c.up.Precreate(ctx, c.token, remotePath, info.Size(), blocks)
	if err != nil {
		return err
	}
	for i, p := range parts {
		if err := c.up.UploadPart(ctx, c.token, remotePath, uploadID, i, p); err != nil {
			return err
		}
	}
	fsID, sum, errno, err := c.up.Create(ctx, c.token, remotePath, uploadID, info.Size(), blocks)
	if err != nil {
		return err
	}
	if c.OnCreated != nil {
		c.OnCreated(info.Size(), remotePath, fsID, sum, errno)
	}
	if errno != 0 {
		return fmt.Errorf("百度返回错误 errno=%d", errno)
	}
	return nil
}

// RemotePath 返回 /bak/YYYY-MM-DD/文件名（使用本地日期）。
func RemotePath(localPath string, now time.Time) string {
	return "/bak/" + now.Format("2006-01-02") + "/" + filepath.Base(localPath)
}

// historyRecord 构造上传历史。现有 size 列为 int32，超出范围时记为 0，fs_id 仍保留。
func historyRecord(size int64, remote, fsID, md5 string, errno int) *model.UploadHistory {
	var s int32
	if size >= 0 && size <= math.MaxInt32 {
		s = int32(size)
	}
	return &model.UploadHistory{Size: s, Path: remote, FsId: fsID, Md5: md5, Errno: int32(errno)}
}

// SaveHistory 写入 model.UploadHistory。
func SaveHistory(size int64, remote, fsID, md5 string, errno int) {
	h := historyRecord(size, remote, fsID, md5, errno)
	if h.Create(h) {
		fmt.Println("写入数据库成功")
	} else {
		fmt.Println("写入数据库失败")
	}
}
