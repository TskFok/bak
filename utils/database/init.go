package database

import (
	"bak/global"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func InitMysql() *gorm.DB {
	if err := EnsureDatabase(global.MysqlDsn); err != nil {
		panic("fail to create mysql database: " + err.Error())
	}
	db, err := gorm.Open(mysql.Open(global.MysqlDsn), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			TablePrefix:   global.MysqlPrefix, //前缀
			SingularTable: true,               //复数表名
		},
	})

	if nil != err {
		panic("fail to open mysql connect ")
	}

	return db
}
