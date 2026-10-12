package webdav

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type Client struct {
	base     string
	username string
	password string
	http     *http.Client
}

func New(url, username, password string) *Client {
	return &Client{base: strings.TrimRight(url, "/"), username: username, password: password, http: http.DefaultClient}
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

func (c *Client) EnsureDir(ctx context.Context, remoteDir string) error {
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", c.base+remoteDir, nil)
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
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}
	mk, err := http.NewRequestWithContext(ctx, "MKCOL", c.base+remoteDir, nil)
	if err != nil {
		return err
	}
	mk.SetBasicAuth(c.username, c.password)
	res, err = c.http.Do(mk)
	if err != nil {
		return fmt.Errorf("无法访问")
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusCreated || res.StatusCode == http.StatusMethodNotAllowed || (res.StatusCode >= 200 && res.StatusCode < 300) {
		return nil
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.base+remotePath, f)
	if err != nil {
		return err
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
