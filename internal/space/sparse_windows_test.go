//go:build windows

package space

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
)

// 用真实稀疏文件验证逻辑大小不会冒充物理占用。
func TestSparseFileUsesAllocatedSize(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sparse.bin")
	f, e := os.Create(p)
	if e != nil {
		t.Fatal(e)
	}
	var returned uint32
	e = windows.DeviceIoControl(windows.Handle(f.Fd()), windows.FSCTL_SET_SPARSE, nil, 0, nil, 0, &returned, nil)
	if e != nil {
		f.Close()
		t.Skip("测试卷不支持稀疏文件", e)
	}
	if e = f.Truncate(256 << 20); e != nil {
		t.Fatal(e)
	}
	f.Close()
	_, allocated, e := nativeStat(p)
	if e != nil || allocated >= 256<<20 || allocated < 0 {
		t.Fatalf("稀疏文件分配错误: %d %v", allocated, e)
	}
}
