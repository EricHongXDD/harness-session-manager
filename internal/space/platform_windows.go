//go:build windows

package space

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"syscall"
	"unsafe"
)

func reparse(i os.FileInfo) bool {
	d, ok := i.Sys().(*syscall.Win32FileAttributeData)
	return ok && d.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
func nativeStat(path string) (string, int64, error) {
	p, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return "", 0, e
	}
	h, e := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if e != nil {
		return "", 0, e
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if e = windows.GetFileInformationByHandle(h, &info); e != nil {
		return "", 0, e
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return "", 0, fmt.Errorf("重解析点不能计量")
	}
	var standard struct {
		Allocation int64
		End        int64
		Links      uint32
		Delete     uint8
		Directory  uint8
		Pad        [2]byte
	}
	e = windows.GetFileInformationByHandleEx(h, windows.FileStandardInfo, (*byte)(unsafe.Pointer(&standard)), uint32(unsafe.Sizeof(standard)))
	if e != nil {
		return "", 0, e
	}
	allocated := standard.Allocation
	// 压缩和稀疏文件使用同一原生句柄的实际压缩分配信息。
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_COMPRESSED|windows.FILE_ATTRIBUTE_SPARSE_FILE) != 0 {
		var compression struct {
			Size                 int64
			Format               uint16
			Unit, Chunk, Cluster uint8
			Reserved             [3]byte
		}
		if e = windows.GetFileInformationByHandleEx(h, windows.FileCompressionInfo, (*byte)(unsafe.Pointer(&compression)), uint32(unsafe.Sizeof(compression))); e != nil {
			return "", 0, e
		}
		allocated = compression.Size
	}
	if allocated < 0 {
		return "", 0, fmt.Errorf("文件分配大小未知")
	}
	return fmt.Sprintf("%08x:%08x%08x", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), allocated, nil
}

// 不允许覆盖既有目标；不同卷移动失败时保留原目录。
func moveNoReplace(from, to string) error {
	a, e := windows.UTF16PtrFromString(from)
	if e != nil {
		return e
	}
	b, e := windows.UTF16PtrFromString(to)
	if e != nil {
		return e
	}
	return windows.MoveFileEx(a, b, 0)
}
