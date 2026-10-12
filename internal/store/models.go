package store

import (
	"time"

	"BaiduYunPanBak/global"
	"BaiduYunPanBak/model"

	"gorm.io/gorm"
)

// touch 补全 BaseModel 的时间列。BaseModel 使用 LocalTime，GORM 不会自动维护。
func touch(b *model.BaseModel) {
	now := model.LocalTime(time.Now())
	if b.CreatedAt == nil {
		t := now
		b.CreatedAt = &t
	}
	t := now
	b.UpdatedAt = &t
}

func tableName(name string) string { return global.MysqlPrefix + name }

// User 对应 users 表，全表只允许一行。
type User struct {
	model.BaseModel
	ID           uint   `gorm:"column:id;primaryKey;autoIncrement"`
	Username     string `gorm:"column:username;type:VARCHAR(32);uniqueIndex;NOT NULL"`
	PasswordHash string `gorm:"column:password_hash;type:VARCHAR(255);NOT NULL"`
}

func (User) TableName() string { return tableName("users") }

func (m *User) BeforeCreate(*gorm.DB) error { touch(&m.BaseModel); return nil }

// CloudAccount 对应 cloud_accounts 表，每个 provider 一行。
type CloudAccount struct {
	model.BaseModel
	ID          uint   `gorm:"column:id;primaryKey;autoIncrement"`
	Provider    string `gorm:"column:provider;type:VARCHAR(32);uniqueIndex;NOT NULL"`
	DisplayName string `gorm:"column:display_name;type:VARCHAR(255);NOT NULL"`
	SecretBlob  string `gorm:"column:secret_blob;type:TEXT"`
	Status      string `gorm:"column:status;type:VARCHAR(32);NOT NULL"`
	LastError   string `gorm:"column:last_error;type:TEXT"`
}

func (CloudAccount) TableName() string { return tableName("cloud_accounts") }

func (m *CloudAccount) BeforeCreate(*gorm.DB) error { touch(&m.BaseModel); return nil }

// backupTask 对应 backup_tasks 表。
type backupTask struct {
	model.BaseModel
	ID              uint       `gorm:"column:id;primaryKey;autoIncrement"`
	Name            string     `gorm:"column:name;type:VARCHAR(255);NOT NULL"`
	LocalDir        string     `gorm:"column:local_dir;type:VARCHAR(1024);NOT NULL"`
	CloudAccountID  uint       `gorm:"column:cloud_account_id;NOT NULL"`
	RemoteDir       string     `gorm:"column:remote_dir;type:VARCHAR(1024);NOT NULL"`
	ScheduleTime    string     `gorm:"column:schedule_time;type:VARCHAR(5);NOT NULL"`
	Enabled         bool       `gorm:"column:enabled;NOT NULL"`
	LastScheduledOn *time.Time `gorm:"column:last_scheduled_on;type:DATE"`
}

func (backupTask) TableName() string { return tableName("backup_tasks") }

func (m *backupTask) BeforeCreate(*gorm.DB) error { touch(&m.BaseModel); return nil }

// backupRun 对应 backup_runs 表。
type backupRun struct {
	model.BaseModel
	ID           uint       `gorm:"column:id;primaryKey;autoIncrement"`
	TaskID       uint       `gorm:"column:task_id;index;NOT NULL"`
	Trigger      string     `gorm:"column:trigger;type:VARCHAR(16);NOT NULL"`
	Status       string     `gorm:"column:status;type:VARCHAR(16);index;NOT NULL"`
	StartedAt    time.Time  `gorm:"column:started_at;type:DATETIME;NOT NULL"`
	FinishedAt   *time.Time `gorm:"column:finished_at;type:DATETIME"`
	SuccessCount int        `gorm:"column:success_count;NOT NULL"`
	SkipCount    int        `gorm:"column:skip_count;NOT NULL"`
	FailCount    int        `gorm:"column:fail_count;NOT NULL"`
	ErrorSummary string     `gorm:"column:error_summary;type:TEXT"`
}

func (backupRun) TableName() string { return tableName("backup_runs") }

func (m *backupRun) BeforeCreate(*gorm.DB) error { touch(&m.BaseModel); return nil }

// backupFileResult 对应 backup_file_results 表。
type backupFileResult struct {
	model.BaseModel
	ID                uint      `gorm:"column:id;primaryKey;autoIncrement"`
	RunID             uint      `gorm:"column:run_id;index;NOT NULL"`
	TaskID            uint      `gorm:"column:task_id;NOT NULL;index:idx_backup_file_results_lookup,priority:1"`
	RelativePath      string    `gorm:"column:relative_path;type:VARCHAR(512);NOT NULL;index:idx_backup_file_results_lookup,priority:2"`
	Size              int64     `gorm:"column:size;NOT NULL"`
	ModTime           time.Time `gorm:"column:mod_time;type:DATETIME;NOT NULL"`
	Status            string    `gorm:"column:status;type:VARCHAR(16);NOT NULL"`
	Error             string    `gorm:"column:error;type:TEXT"`
	SourceFingerprint string    `gorm:"column:source_fingerprint;type:VARCHAR(64);NOT NULL;index:idx_backup_file_results_lookup,priority:3"`
}

func (backupFileResult) TableName() string { return tableName("backup_file_results") }

func (m *backupFileResult) BeforeCreate(*gorm.DB) error { touch(&m.BaseModel); return nil }

// AutoMigrate 创建或更新本任务新增的五张表。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&User{}, &CloudAccount{}, &backupTask{}, &backupRun{}, &backupFileResult{})
}
