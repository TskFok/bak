package store

import (
	"math"
	"testing"
	"time"

	"BaiduYunPanBak/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// TestAutoMigrateCreatesMissingLegacyTables 复现空库启动：
// 单数表名下应补齐 token 与 upload_history，再次迁移不能失败。
func TestAutoMigrateCreatesMissingLegacyTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:legacy-tables?mode=memory&cache=shared"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}

	var n int64
	if err := db.Model(&model.Token{}).Count(&n).Error; err != nil {
		t.Fatalf("token table: %v", err)
	}
	if err := db.Model(&model.UploadHistory{}).Count(&n).Error; err != nil {
		t.Fatalf("upload_history table: %v", err)
	}

	now := model.LocalTime(time.Now())
	row := model.Token{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		ExpiresIn:    3600,
		BaseModel:    model.BaseModel{CreatedAt: &now, UpdatedAt: &now},
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	var got model.Token
	if err := db.First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.Id == 0 || got.AccessToken != "access-1" || got.RefreshToken != "refresh-1" {
		t.Fatalf("token row: %+v", got)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("second AutoMigrate: %v", err)
	}
	if err := db.First(&got).Error; err != nil {
		t.Fatalf("row lost after second migrate: %v", err)
	}

	huge := int64(math.MaxInt32) + 9
	hist := model.UploadHistory{
		Path: "/big", Size: huge, FsId: "fs", Md5: "md", Errno: 0,
		BaseModel: model.BaseModel{CreatedAt: &now, UpdatedAt: &now},
	}
	if err := db.Create(&hist).Error; err != nil {
		t.Fatal(err)
	}
	var gotHist model.UploadHistory
	if err := db.First(&gotHist).Error; err != nil {
		t.Fatal(err)
	}
	if gotHist.Size != huge {
		t.Fatalf("size = %d", gotHist.Size)
	}
}
