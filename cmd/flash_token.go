package cmd

import (
	"BaiduYunPanBak/global"
	"BaiduYunPanBak/internal/baidutoken"
	"BaiduYunPanBak/tool/openapi"
	"context"
	"fmt"
	"github.com/spf13/cobra"
)

// flashTokenCmd represents the flashToken command
var flashTokenCmd = &cobra.Command{
	Use:   "flashToken",
	Short: "更新百度云盘token脚本",
	Long:  "更新百度云盘token脚本",
	Run: func(cmd *cobra.Command, args []string) {
		//获取code
		code := args[0]
		clientId := global.BaiduYunClientId
		clientSecret := global.BaiduYunClientSecret
		redirectUri := "oob"

		configuration := openapi.NewConfiguration()
		apiClient := openapi.NewAPIClient(configuration)

		resp, r, err := apiClient.AuthApi.OauthTokenCode2token(context.Background()).Code(code).ClientId(clientId).ClientSecret(clientSecret).RedirectUri(redirectUri).Execute()
		if err != nil {
			fmt.Printf("Error when calling `AuthApi.OauthTokenCode2token``: %v\n", err)
			fmt.Printf("Full HTTP response: %v\n", r)

			return
		}
		fmt.Printf("获取access_token %s", *resp.AccessToken)
		fmt.Printf("获取refresh_token %s", *resp.RefreshToken)
		fmt.Printf("获取session_key %s", *resp.SessionKey)
		fmt.Printf("获取scope %s", *resp.Scope)
		fmt.Printf("获取session_secret %s", *resp.SessionSecret)
		fmt.Printf("获取expires_in %d", *resp.ExpiresIn)

		if err := baidutoken.Save(resp.GetAccessToken(), resp.GetRefreshToken(), int(resp.GetExpiresIn()), saveBaiduAccount); err != nil {
			fmt.Printf("获取token失败: %v\n", err)
			return
		}
		fmt.Printf("获取token成功")
	},
}

func init() {
	rootCmd.AddCommand(flashTokenCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// flashTokenCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// flashTokenCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
