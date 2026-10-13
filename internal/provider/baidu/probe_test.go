package baidu

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestProbeAcceptsBaiduUInfoShape(t *testing.T) {
	const body = `{"errno":0,"errmsg":"succ","uk":2601495096,"request_id":1234567890123456789,"baidu_name":"user","netdisk_name":"disk","vip_type":2}`
	up := OpenAPIUploader{httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.URL.RawQuery, "method=uinfo") {
			t.Fatalf("path %s", r.URL.String())
		}
		if r.URL.Query().Get("access_token") != "token" {
			t.Fatalf("token %q", r.URL.Query().Get("access_token"))
		}
		return jsonResponse(http.StatusOK, body), nil
	})}}
	if err := up.Probe(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
}

func TestProbeRejectsNonZeroErrno(t *testing.T) {
	up := OpenAPIUploader{httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"errno":-6,"errmsg":"no","request_id":1}`), nil
	})}}
	err := up.Probe(context.Background(), "token")
	if err == nil || err.Error() != "授权失败" {
		t.Fatalf("err %v", err)
	}
}
