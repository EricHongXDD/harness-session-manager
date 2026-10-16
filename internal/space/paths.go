package space

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func newID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func within(root, path string) bool {
	r, e := filepath.Rel(root, path)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) && !filepath.IsAbs(r)
}
func safeRelative(path string) bool {
	if path == "" || path == "." || filepath.IsAbs(path) || strings.ContainsAny(path, ":\\\x00<>|?*\r\n") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." || strings.TrimRight(part, ". ") != part {
			return false
		}
		stem := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if stem == "CON" || stem == "NUL" || stem == "AUX" || stem == "PRN" || len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9' {
			return false
		}
	}
	return true
}
func noLinks(path string) error {
	abs, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	volume := filepath.VolumeName(abs)
	current := volume + string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(abs, current), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 || reparse(info) {
			return fmt.Errorf("路径包含链接或重解析点，已停止：%s", current)
		}
	}
	return nil
}
func regular(path string) (os.FileInfo, error) {
	if e := noLinks(path); e != nil {
		return nil, e
	}
	i, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !i.Mode().IsRegular() {
		return nil, fmt.Errorf("不是普通文件：%s", path)
	}
	return i, nil
}

// 写入完整临时文件后原子替换本应用记录，来源文件不经过此函数。
func atomicWrite(path string, data []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".record-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}
