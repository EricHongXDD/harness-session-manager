//go:build !windows

package main

import "fmt"

func openFolder(path string) error { return fmt.Errorf("请手动打开：%s", path) }
