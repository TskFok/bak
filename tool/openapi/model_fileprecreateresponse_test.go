package openapi

import (
	"encoding/json"
	"testing"
)

func TestFileprecreateresponseUnmarshalNumericBlockList(t *testing.T) {
	const body = `{"errno":0,"return_type":1,"uploadid":"N1-upload","block_list":[0,1,2],"request_id":123}`
	var resp Fileprecreateresponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal numeric block_list: %v", err)
	}
	if got := resp.GetUploadid(); got != "N1-upload" {
		t.Fatalf("uploadid = %q", got)
	}
	if got := resp.GetReturnType(); got != 1 {
		t.Fatalf("return_type = %d", got)
	}
	got := resp.GetBlockList()
	want := []int32{0, 1, 2}
	if len(got) != len(want) {
		t.Fatalf("block_list = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("block_list = %v, want %v", got, want)
		}
	}
}
