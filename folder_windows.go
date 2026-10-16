//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

func openFolder(path string) error {
	verb, e := windows.UTF16PtrFromString("open")
	if e != nil {
		return e
	}
	name, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return e
	}
	return windows.ShellExecute(0, verb, name, nil, nil, windows.SW_SHOWNORMAL)
}
