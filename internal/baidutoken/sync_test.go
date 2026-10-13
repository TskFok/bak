package baidutoken

import (
	"testing"
	"time"

	"BaiduYunPanBak/global"
	"BaiduYunPanBak/internal/cloudacct"
	"BaiduYunPanBak/internal/store"
	"BaiduYunPanBak/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestReconcileCopiesCloudTokenIntoEmptyRow(t *testing.T) {
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	cloud := cloudacct.Secrets{
		ClientID: "cid", ClientSecret: "sec", DeviceID: "dev",
		AccessToken: "a-cloud", RefreshToken: "r-cloud",
		ExpiresAt: now.Add(2 * time.Hour),
	}
	nextCloud, nextRow, cloudChanged, rowChanged := Reconcile(cloud, model.Token{}, now)
	if cloudChanged {
		t.Fatal("cloud already has the token")
	}
	if !rowChanged || nextRow.AccessToken != "a-cloud" || nextRow.RefreshToken != "r-cloud" || nextRow.ExpiresIn != 7200 {
		t.Fatalf("row changed=%v %+v", rowChanged, nextRow)
	}
	if nextCloud.ClientID != "cid" || nextCloud.DeviceID != "dev" || nextCloud.ClientSecret != "sec" {
		t.Fatalf("%+v", nextCloud)
	}
}

func TestReconcileCopiesRowIntoEmptyCloud(t *testing.T) {
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	updated := model.LocalTime(now.Add(-time.Hour))
	row := model.Token{
		Id: 3, AccessToken: "a-row", RefreshToken: "r-row", ExpiresIn: 7200,
		BaseModel: model.BaseModel{UpdatedAt: &updated},
	}
	cloud := cloudacct.Secrets{ClientID: "cid", DeviceID: "dev"}
	nextCloud, nextRow, cloudChanged, rowChanged := Reconcile(cloud, row, now)
	if rowChanged || nextRow.Id != 3 {
		t.Fatalf("row changed=%v %+v", rowChanged, nextRow)
	}
	if !cloudChanged || nextCloud.AccessToken != "a-row" || nextCloud.RefreshToken != "r-row" {
		t.Fatalf("%+v", nextCloud)
	}
	if nextCloud.ClientID != "cid" || nextCloud.DeviceID != "dev" {
		t.Fatalf("credentials lost: %+v", nextCloud)
	}
	if !nextCloud.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatal(nextCloud.ExpiresAt)
	}
}

func TestReconcilePrefersLaterCloudExpiry(t *testing.T) {
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	updated := model.LocalTime(now.Add(-30 * time.Minute))
	row := model.Token{
		Id: 1, AccessToken: "a-row", RefreshToken: "r-row", ExpiresIn: 3600,
		BaseModel: model.BaseModel{UpdatedAt: &updated},
	}
	cloud := cloudacct.Secrets{
		ClientID: "cid", DeviceID: "dev",
		AccessToken: "a-cloud", RefreshToken: "r-cloud",
		ExpiresAt: now.Add(3 * time.Hour),
	}
	nextCloud, nextRow, cloudChanged, rowChanged := Reconcile(cloud, row, now)
	if cloudChanged || !rowChanged || nextRow.AccessToken != "a-cloud" || nextRow.Id != 1 {
		t.Fatalf("cloudChanged=%v rowChanged=%v %+v", cloudChanged, rowChanged, nextRow)
	}
	if nextCloud.DeviceID != "dev" || nextCloud.AccessToken != "a-cloud" {
		t.Fatalf("%+v", nextCloud)
	}
}

func TestReconcilePrefersLaterRowExpiry(t *testing.T) {
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	updated := model.LocalTime(now)
	row := model.Token{
		Id: 1, AccessToken: "a-row", RefreshToken: "r-row", ExpiresIn: 10000,
		BaseModel: model.BaseModel{UpdatedAt: &updated},
	}
	cloud := cloudacct.Secrets{
		ClientID: "cid", DeviceID: "dev",
		AccessToken: "a-cloud", RefreshToken: "r-cloud",
		ExpiresAt: now.Add(time.Hour),
	}
	nextCloud, _, cloudChanged, rowChanged := Reconcile(cloud, row, now)
	if !cloudChanged || rowChanged {
		t.Fatalf("cloudChanged=%v rowChanged=%v", cloudChanged, rowChanged)
	}
	if nextCloud.AccessToken != "a-row" || nextCloud.RefreshToken != "r-row" || nextCloud.ClientID != "cid" || nextCloud.DeviceID != "dev" {
		t.Fatalf("%+v", nextCloud)
	}
	if !nextCloud.ExpiresAt.Equal(now.Add(10000 * time.Second)) {
		t.Fatal(nextCloud.ExpiresAt)
	}
}

func TestReconcileSameTokenDoesNotRewrite(t *testing.T) {
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	updated := model.LocalTime(now.Add(-time.Minute))
	row := model.Token{
		Id: 1, AccessToken: "a", RefreshToken: "r", ExpiresIn: 3660,
		BaseModel: model.BaseModel{UpdatedAt: &updated},
	}
	cloud := cloudacct.Secrets{ClientID: "cid", AccessToken: "a", RefreshToken: "r", ExpiresAt: now.Add(time.Hour)}
	_, _, cloudChanged, rowChanged := Reconcile(cloud, row, now)
	if cloudChanged || rowChanged {
		t.Fatalf("cloudChanged=%v rowChanged=%v", cloudChanged, rowChanged)
	}
}

func TestReconcileFillsMissingCloudExpiry(t *testing.T) {
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	updated := model.LocalTime(now)
	row := model.Token{
		AccessToken: "a", RefreshToken: "r", ExpiresIn: 100,
		BaseModel: model.BaseModel{UpdatedAt: &updated},
	}
	cloud := cloudacct.Secrets{ClientID: "cid", AccessToken: "a", RefreshToken: "r"}
	next, _, cloudChanged, rowChanged := Reconcile(cloud, row, now)
	if rowChanged || !cloudChanged {
		t.Fatalf("cloudChanged=%v rowChanged=%v", cloudChanged, rowChanged)
	}
	if next.ClientID != "cid" || !next.ExpiresAt.Equal(now.Add(100*time.Second)) {
		t.Fatalf("%+v", next)
	}
}

func TestSyncWithoutDatabaseLeavesCloud(t *testing.T) {
	prevDB := global.DataBase
	prevReader := tokenReader
	global.DataBase = nil
	tokenReader = defaultTokenReader
	t.Cleanup(func() {
		global.DataBase = prevDB
		tokenReader = prevReader
	})
	cloud := cloudacct.Secrets{ClientID: "cid", AccessToken: "a"}
	next, changed, err := Sync(cloud)
	if err != nil || changed || next.AccessToken != "a" || next.ClientID != "cid" {
		t.Fatalf("err=%v changed=%v %+v", err, changed, next)
	}
}

func TestCurrentAlignsTokenTableAndCloudAccount(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:token-sync?mode=memory&cache=shared"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	prevDB, prevSecret, prevPrefix := global.DataBase, global.JwtSecret, global.MysqlPrefix
	prevReader, prevWriter, prevCache := tokenReader, tokenWriter, cacheSetter
	global.DataBase = db
	global.JwtSecret = "jwt-secret"
	global.MysqlPrefix = ""
	tokenReader = defaultTokenReader
	tokenWriter = defaultTokenWriter
	var cached string
	cacheSetter = func(key, value string, ttl int) { cached = value }
	t.Cleanup(func() {
		global.DataBase, global.JwtSecret, global.MysqlPrefix = prevDB, prevSecret, prevPrefix
		tokenReader, tokenWriter, cacheSetter = prevReader, prevWriter, prevCache
	})
	if err := store.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}

	exp := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	updated := model.LocalTime(exp.Add(-2 * time.Hour))
	row := model.Token{
		AccessToken: "from-row", RefreshToken: "r-row", ExpiresIn: 7200,
		BaseModel: model.BaseModel{CreatedAt: &updated, UpdatedAt: &updated},
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}

	got, err := Current()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "from-row" || got.RefreshToken != "r-row" || !got.ExpiresAt.Equal(exp) {
		t.Fatalf("%+v", got)
	}
	if cached != "from-row" {
		t.Fatalf("cache = %q", cached)
	}
	var acct store.CloudAccount
	if err := db.Where("provider = ?", "baidu").First(&acct).Error; err != nil {
		t.Fatal(err)
	}
	sec, err := cloudacct.Open(global.JwtSecret, acct.SecretBlob)
	if err != nil {
		t.Fatal(err)
	}
	if sec.AccessToken != "from-row" || sec.RefreshToken != "r-row" || !sec.ExpiresAt.Equal(exp) {
		t.Fatalf("%+v", sec)
	}

	cloudExp := time.Now().Add(5 * time.Hour).UTC().Truncate(time.Second)
	sec.AccessToken, sec.RefreshToken, sec.ExpiresAt = "from-cloud", "r-cloud", cloudExp
	next, changed, err := Sync(sec)
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if next.AccessToken != "from-cloud" {
		t.Fatalf("%+v", next)
	}
	var stored model.Token
	if err := db.First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.AccessToken != "from-cloud" || stored.RefreshToken != "r-cloud" {
		t.Fatalf("%+v", stored)
	}
}

func TestReconcileEmptyBoth(t *testing.T) {
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	_, _, cloudChanged, rowChanged := Reconcile(cloudacct.Secrets{ClientID: "cid"}, model.Token{}, now)
	if cloudChanged || rowChanged {
		t.Fatal("nothing to sync")
	}
}
