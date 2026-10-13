package model

import (
	"bak/global"
	"fmt"
)

type Token struct {
	BaseModel
	Id           uint32 `gorm:"column:id;primaryKey"`
	AccessToken  string `gorm:"column:access_token;size:255;NOT NULL"`
	RefreshToken string `gorm:"column:refresh_token;size:255;NOT NULL"`
	ExpiresIn    int32  `gorm:"column:expires_in;NOT NULL"`
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
