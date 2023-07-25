package model

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"time"
)

type LocalTime time.Time

// MarshalJSON 格式化获取的时间
func (t *LocalTime) MarshalJSON() ([]byte, error) {
	tTime := time.Time(*t)

	return []byte(fmt.Sprintf("\"%v\"", tTime.Format("2006-01-02 15:04:05"))), nil
}

// Value 格式化写入的时间
func (t *LocalTime) Value() (driver.Value, error) {
	return time.Time(*t), nil
}

// Scan 查询前操作
func (t *LocalTime) Scan(v interface{}) error {
	if value, ok := v.(time.Time); ok {
		*t = LocalTime(value)
		return nil
	}

	return errors.New("时间转换出错")
}

type BaseModel struct {
	CreatedAt *LocalTime `json:"created_at" gorm:"column:created_at;type:DATETIME;NOT NULL"`
	UpdatedAt *LocalTime `json:"updated_at" gorm:"column:updated_at;type:DATETIME;NOT NULL"`
}
