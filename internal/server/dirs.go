package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

var errDirUnreadable = errors.New("目录不存在或不可读")

type dirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type dirListing struct {
	Path    string     `json:"path"`
	Parent  string     `json:"parent,omitempty"`
	Entries []dirEntry `json:"entries"`
}

// listDirs 列出本机目录，供任务表单选择本地备份目录。只返回子目录，不返回文件内容。
func (a *api) listDirs(w http.ResponseWriter, req *http.Request) {
	listing, err := listLocalDirs(req.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, listing)
}

// listLocalDirs 返回 requested 下的子目录。requested 为空时从用户主目录开始。
func listLocalDirs(requested string) (dirListing, error) {
	dir := strings.TrimSpace(requested)
	if dir == "" {
		dir = browseStart()
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dirListing{}, errDirUnreadable
	}
	abs = filepath.Clean(abs)
	if !localDirReadable(abs) {
		return dirListing{}, errDirUnreadable
	}
	items, err := os.ReadDir(abs)
	if err != nil {
		return dirListing{}, errDirUnreadable
	}
	entries := []dirEntry{}
	for _, item := range items {
		child := filepath.Join(abs, item.Name())
		if !isDirectory(item, child) {
			continue
		}
		entries = append(entries, dirEntry{Name: item.Name(), Path: child})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	parent := ""
	if p := filepath.Dir(abs); p != abs {
		parent = p
	}
	return dirListing{Path: abs, Parent: parent, Entries: entries}, nil
}

func browseStart() string {
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" && localDirReadable(home) {
		return home
	}
	if runtime.GOOS == "windows" {
		return `C:\`
	}
	return "/"
}

func isDirectory(item os.DirEntry, path string) bool {
	if item.IsDir() {
		return true
	}
	if item.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
