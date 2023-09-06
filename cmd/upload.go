package cmd

import (
	"BaiduYunPanBak/model"
	"BaiduYunPanBak/tool/openapi"
	"BaiduYunPanBak/utils/cache"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"os"
	"path"
	"strconv"
	"time"
)

// uploadCmd represents the upload command
var uploadCmd = &cobra.Command{
	Use:   "upload",
	Short: "上传到百度云盘",
	Long:  "上传到百度云盘",
	Run: func(cmd *cobra.Command, args []string) {
		token := cache.Get("access_token")

		if token == "" {
			tk := model.Token{}
			tokenInfo := tk.Find()
			token = tokenInfo.AccessToken
		}

		//文件绝对路径
		filePath := args[0]
		//文件名
		fileName := path.Base(filePath)
		//存放分割文件的路径
		savePath := args[1]

		file, err := os.Open(filePath)

		if err != nil {
			fmt.Println(err.Error())

			return
		}

		defer file.Close()

		data, err := io.ReadAll(file)

		if err != nil {
			fmt.Println(err.Error())

			return
		}

		//分割需要上传的文件,大小为4MB
		chunkSize := 4194394
		num := len(data) / 4194394

		blockList := make([]string, num+1)
		filePathList := make([]string, num+1)

		for i := 0; i < num+1; i++ {
			start := chunkSize * i
			end := start + chunkSize

			if i == num {
				end = len(data)
			}

			dataFile := data[start:end]
			//将文件分割成chunk0.dat,chunk1.dat的切片
			err := os.WriteFile(fmt.Sprintf(savePath+"/chunk%d.dat", i), dataFile, 0644)

			if err != nil {
				fmt.Printf("分片错误 %s", err.Error())

				return
			}

			filePathList[i] = fmt.Sprintf(savePath+"/chunk%d.dat", i)
			blockList[i] = md5string(dataFile)
		}

		bt, err := json.Marshal(blockList)
		if err != nil {
			fmt.Println(err.Error())

			return
		}

		//上传到的云盘地址 /bak/当前日期/文件名
		bakPath := "/bak/" + time.Now().Format("2006-01-02") + "/" + fileName
		//预上传
		uplodidTmp := panfileprecreate(token, len(data), string(bt), bakPath)

		//分片上传
		for z := 0; z < num+1; z++ {
			pcssuperfile2(token, uplodidTmp, strconv.Itoa(z), filePathList[z], bakPath)
		}

		panfilecreate(token, uplodidTmp, len(data), string(bt), bakPath)
	},
}

func md5string(dataFile []byte) string {
	return fmt.Sprintf("%x", md5.Sum(dataFile))
}

/** 创建文件*/
func panfilecreate(accessToken string, uplodidTmp string, size int, blockList string, bakPath string) {
	isdir := int32(0)
	size32 := int32(size)
	uploadid := uplodidTmp
	rtype := int32(3)

	configuration := openapi.NewConfiguration()
	apiClient := openapi.NewAPIClient(configuration)
	resp, r, err := apiClient.FileuploadApi.Xpanfilecreate(context.Background()).AccessToken(accessToken).Path(bakPath).Isdir(isdir).Size(size32).Uploadid(uploadid).BlockList(blockList).Rtype(rtype).Execute()
	if err != nil {
		fmt.Printf("Error when calling `FileuploadApi.Xpanfilecreate``: %v\n", err)
		fmt.Printf("Full HTTP response: %v\n", r)
	}

	hs := &model.UploadHistory{
		Size:  *resp.Size,
		Path:  *resp.Path,
		FsId:  strconv.FormatInt(*resp.FsId, 10),
		Md5:   *resp.Md5,
		Errno: *resp.Errno,
	}

	res := hs.Create(hs)

	if res {
		fmt.Println("写入数据库成功")
	} else {
		fmt.Println("写入数据库失败")
	}
}

/** 分片上传*/
func pcssuperfile2(token string, uploadid string, partseq string, localPath string, bakPath string) {
RETRY:
	fmt.Printf("切片%s", partseq)
	accessToken := token
	type_ := "tmpfile"
	file, err := os.Open(localPath)
	if err != nil {
		fmt.Println(err)
	}
	defer file.Close()

	configuration := openapi.NewConfiguration()
	//configuration.Debug = true
	apiClient := openapi.NewAPIClient(configuration)
	resp, r, err := apiClient.FileuploadApi.Pcssuperfile2(context.Background()).AccessToken(accessToken).Partseq(partseq).Path(bakPath).Uploadid(uploadid).Type_(type_).File(file).Execute()
	if err != nil {
		fmt.Printf("Error when calling `FileuploadApi.Pcssuperfile2``: %v\n", err)
		fmt.Printf("Full HTTP response: %v\n", r)

		goto RETRY
	}
	// response from `Pcssuperfile2`: string
	fmt.Printf("Response from `FileuploadApi.Pcssuperfile2`: %v\n", resp)

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		fmt.Printf("err: %v\n", r)
	}

	fmt.Println(string(bodyBytes))
}

/** 预上传*/
func panfileprecreate(token string, size int, blockList string, bakPath string) string {
	accessToken := token // string
	isdir := int32(0)    // int32
	autoinit := int32(1) // int32
	rtype := int32(3)    // int32 | rtype (optional)
	size32 := int32(size)

	configuration := openapi.NewConfiguration()
	apiClient := openapi.NewAPIClient(configuration)
	resp, _, err := apiClient.FileuploadApi.Xpanfileprecreate(context.Background()).AccessToken(accessToken).Path(bakPath).Isdir(isdir).Size(size32).Autoinit(autoinit).BlockList(blockList).Rtype(rtype).Execute()
	if err != nil {
		//fmt.Printf("Error when calling `FileuploadApi.Xpanfileprecreate``: %v\n", err)
		//fmt.Printf("Full HTTP response: %v\n", r)
	}

	return *resp.Uploadid
}

func init() {
	rootCmd.AddCommand(uploadCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// uploadCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// uploadCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
