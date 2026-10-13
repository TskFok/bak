package cmd

import (
	"context"

	"bak/global"
	"bak/internal/cloudacct"
	"bak/internal/store"
)

// saveBaiduAccount 把命令行拿到的百度令牌同步到面板的百度账号，供 baidutoken.Save 作为 accountSaver 使用。
// 面板里已有的 device_id 会保留，避免重新授权时丢失。
func saveBaiduAccount(blob string) error {
	ctx := context.Background()
	st := store.NewGorm(global.DataBase)
	next, err := cloudacct.Open(global.JwtSecret, blob)
	if err != nil {
		return err
	}
	acct := store.CloudAccount{Provider: "baidu", DisplayName: "百度网盘"}
	clouds, err := st.ListClouds(ctx)
	if err != nil {
		return err
	}
	for _, c := range clouds {
		if c.Provider != "baidu" {
			continue
		}
		acct.DisplayName = c.DisplayName
		if old, err := cloudacct.Open(global.JwtSecret, c.SecretBlob); err == nil {
			next.DeviceID = old.DeviceID
			if next.ClientID == "" {
				next.ClientID, next.ClientSecret = old.ClientID, old.ClientSecret
			}
		}
		break
	}
	sealed, err := cloudacct.Seal(global.JwtSecret, next)
	if err != nil {
		return err
	}
	acct.SecretBlob, acct.Status = sealed, "connected"
	return st.UpsertCloud(ctx, &acct)
}
