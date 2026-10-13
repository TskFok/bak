package openapi

import (
	"encoding/json"
	"testing"
)

func TestUinforesponseUnmarshalBaiduShape(t *testing.T) {
	const body = `{"errno":0,"errmsg":"succ","uk":2601495096,"request_id":1234567890123456789,"baidu_name":"user","netdisk_name":"disk","vip_type":2,"avatar_url":"https://example.com/a"}`
	var resp Uinforesponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal baidu uinfo: %v", err)
	}
	if resp.GetErrno() != 0 {
		t.Fatalf("errno = %d", resp.GetErrno())
	}
	if resp.GetBaiduName() != "user" || resp.GetNetdiskName() != "disk" {
		t.Fatalf("names %q %q", resp.GetBaiduName(), resp.GetNetdiskName())
	}
	if resp.GetRequestId() != "1234567890123456789" {
		t.Fatalf("request_id = %q", resp.GetRequestId())
	}
}

func TestUinforesponseUnmarshalStringRequestID(t *testing.T) {
	const body = `{"errno":-6,"errmsg":"no","uk":123,"request_id":"abc"}`
	var resp Uinforesponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal string request_id: %v", err)
	}
	if resp.GetErrno() != -6 || resp.GetRequestId() != "abc" || resp.GetUk() != 123 {
		t.Fatalf("got errno=%d request_id=%q uk=%d", resp.GetErrno(), resp.GetRequestId(), resp.GetUk())
	}
}
