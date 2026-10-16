//go:build windows

package space

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

// 仅返回匹配到的进程数量，不输出可能带凭据的完整命令行。
func CheckHarnessStopped() error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	script := `$ErrorActionPreference='Stop';$p=@(Get-CimInstance Win32_Process -Filter "Name LIKE '%DeepSeek%' OR Name='node.exe' OR Name='dsh.exe'");$active=0;$unknown=0;foreach($i in $p){if($i.Name -like '*DeepSeek*' -or $i.Name -eq 'dsh.exe'){$active++}elseif(!$i.CommandLine){$unknown++}elseif($i.CommandLine -match '(?i)deepseek.harness|@deepseek-ai[\\/]dsh|[\\/](?:\.dsh|dsh|dsh-runtimes)[\\/]'){$active++}};@{active=$active;unknown=$unknown}|ConvertTo-Json -Compress`
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, e := cmd.Output()
	if e != nil {
		return fmt.Errorf("无法核对 Harness 进程，请确认已退出后重试")
	}
	var result struct {
		Active  int `json:"active"`
		Unknown int `json:"unknown"`
	}
	if json.Unmarshal(out, &result) != nil || result.Unknown > 0 {
		return fmt.Errorf("部分运行程序状态未知，未修改会话")
	}
	if result.Active > 0 {
		return fmt.Errorf("请先退出 DeepSeek Harness（包括托盘、网页服务和命令行），再执行备份、隐藏、恢复或永久删除")
	}
	return nil
}
