package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bak/internal/provider"

	"github.com/yeka/zip"
)

const zipUTF8Name = 0x800

const (
	// ArchiveEncryptionAES 是 WinZip AES-256。打开时需要 7-Zip 或 Keka。
	ArchiveEncryptionAES = "aes"
	// ArchiveEncryptionZipCrypto 是传统 Zip 加密。Windows 资源管理器可以打开。
	ArchiveEncryptionZipCrypto = "zipcrypto"
)

// NormalizeArchiveEncryption 把空值和未知值收成 aes。
func NormalizeArchiveEncryption(v string) string {
	if v == ArchiveEncryptionZipCrypto {
		return ArchiveEncryptionZipCrypto
	}
	return ArchiveEncryptionAES
}

// fileReadError 表示打包时某个文件读不出来。整次备份失败，不上传不完整的压缩包。
type fileReadError struct{ rel string }

func (e *fileReadError) Error() string { return "无法读取：" + e.rel }

// ArchiveFileName 是上传到网盘的压缩包文件名。
// byDate 为真且 day 非零时，在主文件名后加上当天日期，例如 相册-2026-10-13.zip。
func ArchiveFileName(name string, byDate bool, day time.Time) string {
	base := strings.TrimSpace(name)
	base = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', 0:
			return '_'
		default:
			return r
		}
	}, base)
	if strings.HasSuffix(strings.ToLower(base), ".zip") {
		base = strings.TrimSpace(base[:len(base)-4])
	}
	if base == "" || base == "." || base == ".." {
		base = "backup"
	}
	if byDate && !day.IsZero() {
		base = base + "-" + day.Format("2006-01-02")
	}
	return base + ".zip"
}

type packedFile struct {
	rel   string
	local string
	size  int64
	mod   time.Time
}

func collectPackedFiles(root string) ([]packedFile, error) {
	var files []packedFile
	err := filepath.WalkDir(root, func(local string, d fs.DirEntry, walkErr error) error {
		rel, relErr := filepath.Rel(root, local)
		if relErr != nil {
			return nil
		}
		if rel == "." {
			if walkErr != nil {
				return errRootDirUnreadable
			}
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == ".." || strings.HasPrefix(rel, "../") {
			return &fileReadError{rel: rel}
		}
		if walkErr != nil {
			return &fileReadError{rel: rel}
		}
		if d.IsDir() {
			return nil
		}
		info, err := os.Stat(local)
		if err != nil {
			return &fileReadError{rel: rel}
		}
		// 套接字、管道、设备文件没有可打包的内容。跳过，避免 mysql.sock 这类文件让整次备份失败。
		if !info.Mode().IsRegular() {
			return nil
		}
		files = append(files, packedFile{
			rel:   rel,
			local: local,
			size:  info.Size(),
			mod:   info.ModTime().UTC().Truncate(time.Second),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	return files, nil
}

func packSourceFingerprint(task Task, files []packedFile) string {
	h := sha256.New()
	_, _ = io.WriteString(h, Fingerprint(task.LocalDir, task.CloudAccountID, task.RemoteDir))
	_, _ = io.WriteString(h, "\n")
	sum := sha256.Sum256([]byte(task.ArchivePassword))
	_, _ = h.Write(sum[:])
	enc := "none"
	if task.ArchivePassword != "" {
		enc = NormalizeArchiveEncryption(task.ArchiveEncryption)
	}
	_, _ = io.WriteString(h, "\n"+enc+"\n")
	for _, f := range files {
		fmt.Fprintf(h, "%s\n%d\n%d\n", f.rel, f.size, f.mod.Unix())
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeArchive(dest string, files []packedFile, password, enc string, onFile func(done, total int)) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	werr := writeArchiveEntries(zw, files, password, enc, onFile)
	cerr := zw.Close()
	ferr := f.Close()
	if werr != nil {
		return werr
	}
	if cerr != nil {
		return cerr
	}
	return ferr
}

func writeArchiveEntries(zw *zip.Writer, files []packedFile, password, enc string, onFile func(done, total int)) error {
	for i, file := range files {
		hdr := &zip.FileHeader{
			Name:   file.rel,
			Method: zip.Deflate,
		}
		hdr.SetModTime(file.mod)
		hdr.Flags |= zipUTF8Name
		if password != "" {
			hdr.SetPassword(password)
			if NormalizeArchiveEncryption(enc) == ArchiveEncryptionZipCrypto {
				hdr.SetEncryptionMethod(zip.StandardEncryption)
			} else {
				hdr.SetEncryptionMethod(zip.AES256Encryption)
			}
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		src, err := os.Open(file.local)
		if err != nil {
			return &fileReadError{rel: file.rel}
		}
		_, copyErr := io.Copy(w, src)
		closeErr := src.Close()
		if copyErr != nil || closeErr != nil {
			return &fileReadError{rel: file.rel}
		}
		if onFile != nil {
			onFile(i+1, len(files))
		}
	}
	return nil
}

// executePacked 把整个本地目录打成一个 zip 后上传。
// 目录内容、任务配置和密码都没变时跳过，不重新打包。
// 普通文件读不出来时整次失败，避免上传缺文件的压缩包。
// 套接字等非普通文件在收集阶段跳过。
func executePacked(ctx context.Context, store Store, task Task, p provider.Provider, started Run) (Run, error) {
	run := &started
	finish := func() (Run, error) {
		done := time.Now()
		run.FinishedAt = &done
		run.Status = FinalStatus(run.SuccessCount, run.SkipCount, run.FailCount)
		run.ProgressNote = ""
		if err := store.FinishRun(ctx, *run); err != nil {
			return Run{}, err
		}
		return *run, nil
	}
	note := func(text string) {
		run.ProgressNote = text
		_ = store.UpdateRunProgress(ctx, *run)
	}
	record := func(res FileResult) {
		_ = store.SaveFileResult(ctx, run.ID, task.ID, res)
		_ = store.UpdateRunProgress(ctx, *run)
	}

	info, err := os.Stat(task.LocalDir)
	if err != nil || !info.IsDir() {
		return finishFailed(ctx, store, *run, "本地目录不存在或不可读")
	}
	if err := p.EnsureDir(ctx, task.RemoteDir); err != nil {
		return finishFailed(ctx, store, *run, "无法准备网盘目录")
	}
	files, err := collectPackedFiles(task.LocalDir)
	if err != nil {
		if errors.Is(err, errRootDirUnreadable) {
			return finishFailed(ctx, store, *run, "本地目录不存在或不可读")
		}
		var rd *fileReadError
		if errors.As(err, &rd) {
			return finishFailed(ctx, store, *run, rd.Error())
		}
		return finishFailed(ctx, store, *run, "无法创建压缩包")
	}

	rel := ArchiveFileName(task.Name, task.ArchiveByDate, started.StartedAt)
	fp := packSourceFingerprint(task, files)
	prev, ok, _ := store.LatestSuccess(ctx, task.ID, rel, fp)
	if ok {
		run.SkipCount++
		record(FileResult{
			RelativePath: rel, Status: "skipped", Size: prev.Size, ModTime: prev.ModTime, SourceFingerprint: fp,
		})
		return finish()
	}

	tmp, err := os.CreateTemp("", "bak-pack-*.zip")
	if err != nil {
		return finishFailed(ctx, store, *run, "无法创建压缩包")
	}
	tmpName := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpName)
	if err := writeArchive(tmpName, files, task.ArchivePassword, task.ArchiveEncryption, func(done, total int) {
		if total > 0 {
			note(fmt.Sprintf("正在压缩 %d/%d", done, total))
		}
	}); err != nil {
		var rd *fileReadError
		if errors.As(err, &rd) {
			return finishFailed(ctx, store, *run, rd.Error())
		}
		return finishFailed(ctx, store, *run, "无法创建压缩包")
	}
	st, err := os.Stat(tmpName)
	if err != nil {
		return finishFailed(ctx, store, *run, "无法创建压缩包")
	}
	mod := st.ModTime().UTC().Truncate(time.Second)
	note("正在上传压缩包")
	remote := path.Join(task.RemoteDir, rel)
	if upErr := p.Upload(ctx, tmpName, remote); upErr != nil {
		// 先删临时包再写失败记录。收尾如果卡住，压缩包也不会继续占着磁盘。
		_ = os.Remove(tmpName)
		run.FailCount++
		record(FileResult{
			RelativePath: rel, Status: "failed", Error: upErr.Error(),
			Size: st.Size(), ModTime: mod, SourceFingerprint: fp,
		})
		return finish()
	}
	run.SuccessCount++
	record(FileResult{
		RelativePath: rel, Status: "success", Size: st.Size(), ModTime: mod, SourceFingerprint: fp,
	})
	return finish()
}
