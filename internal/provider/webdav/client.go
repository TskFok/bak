package webdav

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
)

type Client struct {
	base     string
	username string
	password string
	http     *http.Client

	mu   sync.Mutex
	dirs map[string]bool // 已确认存在的集合，key 为 "/" 连接的原始段
}

func New(baseURL, username, password string) *Client {
	return &Client{base: strings.TrimRight(baseURL, "/"), username: username, password: password, http: http.DefaultClient, dirs: map[string]bool{}}
}

func (c *Client) Test(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", c.base+"/", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Depth", "0")
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("无法访问")
	}
	defer res.Body.Close()
	return c.statusError(res.StatusCode)
}

// segments 把远端路径拆成非空段。
func segments(remote string) []string {
	var out []string
	for _, seg := range strings.Split(remote, "/") {
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

// remoteURL 逐段转义路径后拼到 base 后面，base 本身不转义。
func (c *Client) remoteURL(segs []string) string {
	var b strings.Builder
	b.WriteString(c.base)
	for _, seg := range segs {
		b.WriteByte('/')
		b.WriteString(url.PathEscape(seg))
	}
	if len(segs) == 0 {
		b.WriteByte('/')
	}
	return b.String()
}

// EnsureDir 自顶向下逐级 MKCOL，已创建过的目录在本 Client 内不再重复请求。
func (c *Client) EnsureDir(ctx context.Context, remoteDir string) error {
	return c.ensureSegments(ctx, segments(remoteDir))
}

func (c *Client) ensureSegments(ctx context.Context, segs []string) error {
	for i := range segs {
		key := strings.Join(segs[:i+1], "/")
		c.mu.Lock()
		done := c.dirs[key]
		c.mu.Unlock()
		if done {
			continue
		}
		if err := c.mkcol(ctx, segs[:i+1]); err != nil {
			return err
		}
		c.mu.Lock()
		c.dirs[key] = true
		c.mu.Unlock()
	}
	return nil
}

func (c *Client) mkcol(ctx context.Context, segs []string) error {
	req, err := http.NewRequestWithContext(ctx, "MKCOL", c.remoteURL(segs), nil)
	if err != nil {
		return fmt.Errorf("无法访问")
	}
	req.SetBasicAuth(c.username, c.password)
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("无法访问")
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode == http.StatusMethodNotAllowed {
		return nil // 已存在
	}
	return c.statusError(res.StatusCode)
}

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
	segs := segments(remotePath)
	if len(segs) == 0 {
		return fmt.Errorf("无法访问")
	}
	if err := c.ensureSegments(ctx, segs[:len(segs)-1]); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.remoteURL(segs), f)
	if err != nil {
		return fmt.Errorf("无法访问")
	}
	req.ContentLength = info.Size()
	req.SetBasicAuth(c.username, c.password)
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("无法访问")
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	return c.statusError(res.StatusCode)
}

func (c *Client) statusError(code int) error {
	if code >= 200 && code < 300 {
		return nil
	}
	if code == http.StatusUnauthorized || code == http.StatusForbidden {
		return fmt.Errorf("认证失败")
	}
	return fmt.Errorf("无法访问")
}
