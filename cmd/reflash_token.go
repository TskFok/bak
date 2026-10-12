/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"BaiduYunPanBak/global"
	"BaiduYunPanBak/internal/baidutoken"
	"BaiduYunPanBak/model"
	"BaiduYunPanBak/tool/openapi"
	"context"
	"fmt"
	"github.com/spf13/cobra"
)

// reflashTokenCmd represents the reflashToken command
var reflashTokenCmd = &cobra.Command{
	Use:   "reflashToken",
	Short: "刷新token持续时间",
	Long:  "刷新token持续时间",
	Run: func(cmd *cobra.Command, args []string) {
		tk := &model.Token{}
		token := tk.Find()

		refreshToken := token.RefreshToken
		clientId := global.BaiduYunClientId
		clientSecret := global.BaiduYunClientSecret

		configuration := openapi.NewConfiguration()
		apiClient := openapi.NewAPIClient(configuration)
		resp, r, err := apiClient.AuthApi.OauthTokenRefreshToken(context.Background()).RefreshToken(refreshToken).ClientId(clientId).ClientSecret(clientSecret).Execute()
		if err != nil {
			fmt.Printf("Error when calling `AuthApi.OauthTokenRefreshToken``: %v\n", err)
			fmt.Printf("Full HTTP response: %v\n", r)
			return
		}

		refresh := resp.GetRefreshToken()
		if refresh == "" {
			refresh = refreshToken
		}
		if err := baidutoken.Save(resp.GetAccessToken(), refresh, int(resp.GetExpiresIn()), saveBaiduAccount); err != nil {
			fmt.Printf("刷新失败: %v\n", err)
			return
		}
		fmt.Printf("刷新成功")
	},
}

func init() {
	rootCmd.AddCommand(reflashTokenCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// reflashTokenCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// reflashTokenCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
