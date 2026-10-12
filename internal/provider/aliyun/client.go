package aliyun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

var (
	errAuth    = errors.New("授权失败")
	errUpload  = errors.New("上传失败")
	errRequest = errors.New("请求失败")
	errNetwork = errors.New("无法访问")
)

// Token 是阿里云盘 OAuth 返回的令牌。
type Token struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// Client 通过阿里云盘官方开放 API 上传文件。
type Client struct {
	base        string
	accessToken string
	http        *http.Client
	driveID     string
}

func New(base, accessToken string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{base: strings.TrimRight(base, "/"), accessToken: accessToken, http: httpClient}
}

func ExchangeCode(ctx context.Context, base, clientID, clientSecret, code string) (Token, error) {
	return tokenRequest(ctx, base, http.DefaultClient, map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     clientID,
		"client_secret": clientSecret,
		"code":          code,
	})
}

func Refresh(ctx context.Context, base, clientID, clientSecret, refreshToken string) (Token, error) {
	return tokenRequest(ctx, base, http.DefaultClient, map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     clientID,
		"client_secret": clientSecret,
		"refresh_token": refreshToken,
	})
}

func tokenRequest(ctx context.Context, base string, hc *http.Client, payload map[string]string) (Token, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return Token{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/oauth/access_token", bytes.NewReader(data))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	now := time.Now()
	res, err := hc.Do(req)
	if err != nil {
		return Token{}, errNetwork
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, res.Body)
		return Token{}, errAuth
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil || out.AccessToken == "" {
		return Token{}, errAuth
	}
	return Token{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		ExpiresAt:    now.Add(time.Duration(out.ExpiresIn) * time.Second),
	}, nil
}

// post 发送带 Bearer 的 JSON 请求。失败时错误文本不含 token。
// failErr 是非 401/403 的非 2xx 状态使用的错误。
func (c *Client) post(ctx context.Context, p string, body, out any, failErr error) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+p, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	res, err := c.http.Do(req)
	if err != nil {
		return errNetwork
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		_, _ = io.Copy(io.Discard, res.Body)
		return errAuth
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, res.Body)
		return failErr
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return failErr
	}
	return nil
}

func (c *Client) getDriveID(ctx context.Context, failErr error) (string, error) {
	if c.driveID != "" {
		return c.driveID, nil
	}
	var out struct {
		DefaultDriveID string `json:"default_drive_id"`
	}
	if err := c.post(ctx, "/adrive/v1.0/user/getDriveInfo", map[string]any{}, &out, failErr); err != nil {
		return "", err
	}
	if out.DefaultDriveID == "" {
		return "", failErr
	}
	c.driveID = out.DefaultDriveID
	return c.driveID, nil
}

func (c *Client) Test(ctx context.Context) error {
	c.driveID = ""
	_, err := c.getDriveID(ctx, errRequest)
	return err
}

type fileItem struct {
	FileID string `json:"file_id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
}

func (c *Client) findFolder(ctx context.Context, driveID, parentID, name string, failErr error) (string, error) {
	marker := ""
	for {
		req := map[string]any{
			"drive_id":       driveID,
			"parent_file_id": parentID,
			"limit":          100,
		}
		if marker != "" {
			req["marker"] = marker
		}
		var out struct {
			Items      []fileItem `json:"items"`
			NextMarker string     `json:"next_marker"`
		}
		if err := c.post(ctx, "/adrive/v1.0/openFile/list", req, &out, failErr); err != nil {
			return "", err
		}
		for _, it := range out.Items {
			if it.Name == name && it.Type == "folder" {
				return it.FileID, nil
			}
		}
		if out.NextMarker == "" {
			return "", nil
		}
		marker = out.NextMarker
	}
}

// ensurePath 从 root 起逐段确认目录存在，返回叶子目录的 file_id。
func (c *Client) ensurePath(ctx context.Context, remoteDir string, failErr error) (string, error) {
	driveID, err := c.getDriveID(ctx, failErr)
	if err != nil {
		return "", err
	}
	parent := "root"
	for _, seg := range strings.Split(path.Clean("/"+remoteDir), "/") {
		if seg == "" {
			continue
		}
		id, err := c.findFolder(ctx, driveID, parent, seg, failErr)
		if err != nil {
			return "", err
		}
		if id == "" {
			var out fileItem
			err := c.post(ctx, "/adrive/v1.0/openFile/create", map[string]any{
				"drive_id":        driveID,
				"parent_file_id":  parent,
				"name":            seg,
				"type":            "folder",
				"check_name_mode": "refuse",
			}, &out, failErr)
			if err != nil {
				return "", err
			}
			if out.FileID == "" {
				return "", failErr
			}
			id = out.FileID
		}
		parent = id
	}
	return parent, nil
}

func (c *Client) EnsureDir(ctx context.Context, remoteDir string) error {
	_, err := c.ensurePath(ctx, remoteDir, errRequest)
	return err
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
	clean := path.Clean("/" + remotePath)
	dir, name := path.Split(clean)
	if name == "" {
		return errUpload
	}
	parentID, err := c.ensurePath(ctx, dir, errUpload)
	if err != nil {
		return err
	}
	driveID, err := c.getDriveID(ctx, errUpload)
	if err != nil {
		return err
	}
	var created struct {
		FileID       string `json:"file_id"`
		UploadID     string `json:"upload_id"`
		PartInfoList []struct {
			UploadURL string `json:"upload_url"`
		} `json:"part_info_list"`
	}
	err = c.post(ctx, "/adrive/v1.0/openFile/create", map[string]any{
		"drive_id":        driveID,
		"parent_file_id":  parentID,
		"name":            name,
		"type":            "file",
		"check_name_mode": "auto_rename",
		"size":            info.Size(),
		"part_info_list":  []map[string]any{{"part_number": 1}},
	}, &created, errUpload)
	if err != nil {
		return err
	}
	if created.FileID == "" || created.UploadID == "" ||
		len(created.PartInfoList) == 0 || created.PartInfoList[0].UploadURL == "" {
		return errUpload
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, created.PartInfoList[0].UploadURL, f)
	if err != nil {
		return errUpload
	}
	req.ContentLength = info.Size()
	res, err := c.http.Do(req)
	if err != nil {
		return errNetwork
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return errUpload
	}
	// 官方流程：所有分片 PUT 成功后必须调用 complete，文件才算上传完成。
	return c.post(ctx, "/adrive/v1.0/openFile/complete", map[string]any{
		"drive_id":  driveID,
		"file_id":   created.FileID,
		"upload_id": created.UploadID,
	}, nil, errUpload)
}
