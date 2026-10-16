//go:build !windows

package space

import (
	"fmt"
	"os"
)

func reparse(i os.FileInfo) bool { return false }
func nativeStat(path string) (string, int64, error) {
	return "", 0, fmt.Errorf("此平台暂未提供物理占用")
}

func moveNoReplace(from, to string) error {
	if _, e := os.Lstat(to); !os.IsNotExist(e) {
		return fmt.Errorf("目标已存在")
	}
	return os.Rename(from, to)
}
