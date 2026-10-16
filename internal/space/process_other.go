//go:build !windows

package space

import "fmt"

func CheckHarnessStopped() error { return fmt.Errorf("当前版本只在 Windows 启用会话移动") }
