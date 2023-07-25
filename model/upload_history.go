package model

import (
	"BaiduYunPanBak/global"
	"fmt"
)

type UploadHistory struct {
	BaseModel
	Id    uint32 `gorm:"column:id;type:INT(10) UNSIGNED;AUTO_INCREMENT;NOT NULL"`
	Path  string `gorm:"column:path;type:VARCHAR(255);NOT NULL"`
	Size  int32  `gorm:"column:size;type:INT(11);NOT NULL"`
	FsId  string `gorm:"column:fs_id;type:VARCHAR(255);NOT NULL"`
	Md5   string `gorm:"column:md5;type:VARCHAR(255);NOT NULL"`
	Errno int32  `gorm:"column:errno;type:INT(11);NOT NULL"`
}

func (UploadHistory) Create(t interface{}) bool {
	db := global.DataBase.Create(t)

	if db.Error != nil {
		fmt.Println(db.Error.Error())
		return false
	}

	return true
}
