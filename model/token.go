package model

import (
	"BaiduYunPanBak/global"
	"fmt"
)

type Token struct {
	BaseModel
	Id           uint32 `gorm:"column:id;type:INT(10) UNSIGNED;AUTO_INCREMENT;NOT NULL"`
	AccessToken  string `gorm:"column:access_token;type:VARCHAR(255);NOT NULL"`
	RefreshToken string `gorm:"column:refresh_token;type:VARCHAR(255);NOT NULL"`
	ExpiresIn    int32  `gorm:"column:expires_in;type:INT(11);NOT NULL"`
}

func (Token) Create(t interface{}) bool {
	db := global.DataBase.Save(t)

	if db.Error != nil {
		fmt.Println(db.Error.Error())
		return false
	}

	return true
}

func (t Token) Update(condition interface{}, where interface{}) bool {
	db := global.DataBase.Model(t).Where(where).Updates(condition)

	if db.Error != nil {
		fmt.Println(db.Error.Error())
		return false
	}

	return true
}

func (Token) Find() (t Token) {
	db := global.DataBase.First(&t)

	if db.Error != nil {
		fmt.Println(db.Error.Error())
		return
	}

	return
}
