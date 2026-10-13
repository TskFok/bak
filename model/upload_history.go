package model

import (
	"bak/global"
	"fmt"
)

type UploadHistory struct {
	BaseModel
	Id    uint32 `gorm:"column:id;primaryKey"`
	Path  string `gorm:"column:path;size:255;NOT NULL"`
	Size  int64  `gorm:"column:size;NOT NULL"`
	FsId  string `gorm:"column:fs_id;size:255;NOT NULL"`
	Md5   string `gorm:"column:md5;size:255;NOT NULL"`
	Errno int32  `gorm:"column:errno;NOT NULL"`
}

func (UploadHistory) Create(t interface{}) bool {
	db := global.DataBase.Create(t)

	if db.Error != nil {
		fmt.Println(db.Error.Error())
		return false
	}

	return true
}
