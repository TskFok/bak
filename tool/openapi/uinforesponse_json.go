package openapi

import (
	"bytes"
	"encoding/json"
	"math"
)

// UnmarshalJSON 接受百度用户信息接口的实际 JSON。
// 成功响应里 request_id 是数字，uk 经常大于 int32。生成模型把它们标成 string 和 int32，
// 直接解码会失败，测试连接就会把一次成功的调用当成授权失败。
func (o *Uinforesponse) UnmarshalJSON(data []byte) error {
	var raw struct {
		Errno       *int32          `json:"errno"`
		Errmsg      *string         `json:"errmsg"`
		Uk          json.RawMessage `json:"uk"`
		RequestId   json.RawMessage `json:"request_id"`
		AvatarUrl   *string         `json:"avatar_url"`
		BaiduName   *string         `json:"baidu_name"`
		NetdiskName *string         `json:"netdisk_name"`
		VipType     *int32          `json:"vip_type"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	uk, err := flexibleInt32(raw.Uk)
	if err != nil {
		return err
	}
	requestID, err := flexibleString(raw.RequestId)
	if err != nil {
		return err
	}
	*o = Uinforesponse{
		Errno:       raw.Errno,
		Errmsg:      raw.Errmsg,
		Uk:          uk,
		RequestId:   requestID,
		AvatarUrl:   raw.AvatarUrl,
		BaiduName:   raw.BaiduName,
		NetdiskName: raw.NetdiskName,
		VipType:     raw.VipType,
	}
	return nil
}

func flexibleString(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return &s, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var n json.Number
	if err := dec.Decode(&n); err != nil {
		return nil, err
	}
	s = n.String()
	return &s, nil
}

// flexibleInt32 解析 uk。超出 int32 时留空，避免用户 ID 过大导致整段响应解码失败。
func flexibleInt32(raw json.RawMessage) (*int32, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, err
	}
	if n > math.MaxInt32 || n < math.MinInt32 {
		return nil, nil
	}
	v := int32(n)
	return &v, nil
}
