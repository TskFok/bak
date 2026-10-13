package baidu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"

	"bak/tool/openapi"
)

const partRetries = 3

// OpenAPIUploader 通过现有 tool/openapi 客户端调用百度开放接口。
type OpenAPIUploader struct{}

func NewOpenAPIUploader() *OpenAPIUploader { return &OpenAPIUploader{} }

func (OpenAPIUploader) api() *openapi.APIClient {
	return openapi.NewAPIClient(openapi.NewConfiguration())
}

// toInt32 生成的 SDK 的 size 参数为 int32，超出时明确报错而不是溢出。
func toInt32(size int64) (int32, error) {
	if size < 0 || size > math.MaxInt32 {
		return 0, fmt.Errorf("文件大小 %d 超出接口支持的范围", size)
	}
	return int32(size), nil
}

func (u OpenAPIUploader) Precreate(ctx context.Context, token, remote string, size int64, blocks []string) (string, error) {
	size32, err := toInt32(size)
	if err != nil {
		return "", err
	}
	bl, err := json.Marshal(blocks)
	if err != nil {
		return "", err
	}
	resp, _, err := u.api().FileuploadApi.Xpanfileprecreate(ctx).AccessToken(token).Path(remote).
		Isdir(0).Size(size32).Autoinit(1).BlockList(string(bl)).Rtype(3).Execute()
	if err != nil {
		return "", err
	}
	if resp.Uploadid == nil {
		return "", fmt.Errorf("预上传未返回 uploadid")
	}
	return *resp.Uploadid, nil
}

func (u OpenAPIUploader) UploadPart(ctx context.Context, token, remote, uploadID string, seq int, chunkPath string) error {
	var lastErr error
	for i := 0; i < partRetries; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = u.uploadPartOnce(ctx, token, remote, uploadID, seq, chunkPath)
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

func (u OpenAPIUploader) uploadPartOnce(ctx context.Context, token, remote, uploadID string, seq int, chunkPath string) error {
	file, err := os.Open(chunkPath)
	if err != nil {
		return err
	}
	defer file.Close()
	_, _, err = u.api().FileuploadApi.Pcssuperfile2(ctx).AccessToken(token).Partseq(strconv.Itoa(seq)).
		Path(remote).Uploadid(uploadID).Type_("tmpfile").File(file).Execute()
	return err
}

func (u OpenAPIUploader) Create(ctx context.Context, token, remote, uploadID string, size int64, blocks []string) (string, string, int, error) {
	size32, err := toInt32(size)
	if err != nil {
		return "", "", 0, err
	}
	bl, err := json.Marshal(blocks)
	if err != nil {
		return "", "", 0, err
	}
	resp, _, err := u.api().FileuploadApi.Xpanfilecreate(ctx).AccessToken(token).Path(remote).
		Isdir(0).Size(size32).Uploadid(uploadID).BlockList(string(bl)).Rtype(3).Execute()
	if err != nil {
		return "", "", 0, err
	}
	var fsID, sum string
	var errno int
	if resp.FsId != nil {
		fsID = strconv.FormatInt(*resp.FsId, 10)
	}
	if resp.Md5 != nil {
		sum = *resp.Md5
	}
	if resp.Errno != nil {
		errno = int(*resp.Errno)
	}
	return fsID, sum, errno, nil
}

// errnoDirExists 是百度返回的“目录已存在”错误码。
const errnoDirExists = -8

// mkdirResult 判定创建目录的 errno：0 与“已存在”都视为成功，使 Mkdir 幂等。
func mkdirResult(errno int) error {
	if errno == 0 || errno == errnoDirExists {
		return nil
	}
	return fmt.Errorf("创建目录失败 errno=%d", errno)
}

// errnoFromAPIError 从 SDK 的非 2xx 错误体里取出 errno。
func errnoFromAPIError(err error) (int, bool) {
	var apiErr openapi.GenericOpenAPIError
	if !errors.As(err, &apiErr) {
		return 0, false
	}
	var body struct {
		Errno *int `json:"errno"`
	}
	if json.Unmarshal(apiErr.Body(), &body) != nil || body.Errno == nil {
		return 0, false
	}
	return *body.Errno, true
}

// redactToken 去掉错误文本中可能出现的 access_token（传输错误会带上完整 URL）。
func redactToken(msg, token string) string {
	if token == "" {
		return msg
	}
	msg = strings.ReplaceAll(msg, token, "***")
	return strings.ReplaceAll(msg, url.QueryEscape(token), "***")
}

func (u OpenAPIUploader) Mkdir(ctx context.Context, token, remote string) error {
	resp, _, err := u.api().FileuploadApi.Xpanfilecreate(ctx).AccessToken(token).Path(remote).
		Isdir(1).Size(0).Uploadid("").BlockList("[]").Rtype(0).Execute()
	if err != nil {
		if errno, ok := errnoFromAPIError(err); ok {
			return mkdirResult(errno)
		}
		return fmt.Errorf("创建目录失败: %s", redactToken(err.Error(), token))
	}
	if resp.Errno == nil {
		return nil
	}
	return mkdirResult(int(*resp.Errno))
}

// Probe 调用用户信息接口，确认 token 可用。错误文本不含 token。
func (u OpenAPIUploader) Probe(ctx context.Context, token string) error {
	resp, _, err := u.api().UserinfoApi.Xpannasuinfo(ctx).AccessToken(token).Execute()
	if err != nil {
		return errors.New("授权失败")
	}
	if resp.Errno != nil && *resp.Errno != 0 {
		return errors.New("授权失败")
	}
	return nil
}
