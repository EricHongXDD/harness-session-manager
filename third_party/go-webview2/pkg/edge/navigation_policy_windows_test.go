//go:build windows

package edge

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SHStrDupW通过COM分配器生成真实LPWSTR，生产getter仍实际负责CoTaskMemFree。
func navigationTestURI(t *testing.T, text string, calls *int) ComProc {
	t.Helper()
	dup := windows.NewLazySystemDLL("shlwapi.dll").NewProc("SHStrDupW")
	return NewComProc(func(_ unsafe.Pointer, out **uint16) uintptr {
		*calls++
		if out == nil {
			t.Error("LPWSTR必须传非空输出地址")
			return 0x80004003
		}
		if *out != nil {
			t.Error("输出变量必须初始化为nil")
		}
		value, err := windows.UTF16PtrFromString(text)
		if err != nil {
			t.Error(err)
			return 0x80004005
		}
		hr, _, _ := dup.Call(uintptr(unsafe.Pointer(value)), uintptr(unsafe.Pointer(out)))
		runtime.KeepAlive(value)
		return hr
	})
}

func TestNavigationPolicyBaseABISlotsAndBOOLValues(t *testing.T) {
	width := unsafe.Sizeof(uintptr(0))
	if unsafe.Offsetof(navigationStartingArgsVtbl{}.GetURI) != 3*width || unsafe.Offsetof(navigationStartingArgsVtbl{}.PutCancel) != 8*width || unsafe.Offsetof(newWindowArgsVtbl{}.PutHandled) != 6*width || unsafe.Offsetof(navigationHandlerVtbl{}.Invoke) != 3*width {
		t.Fatal("基础IUnknown继承或事件槽位错误")
	}
	if unsafe.Offsetof(iCoreWebView2Vtbl{}.AddNavigationStarting) != 7*width || unsafe.Offsetof(iCoreWebView2Vtbl{}.AddFrameNavigationStarting) != 17*width || unsafe.Offsetof(iCoreWebView2Vtbl{}.AddNewWindowRequested) != 44*width {
		t.Fatal("不能把已有ICoreWebView2解释成高版本短vtable")
	}
	calls := 0
	get := navigationTestURI(t, "http://wails.localhost/中文#section", &calls)
	args := navigationStartingArgs{vtbl: &navigationStartingArgsVtbl{GetURI: get}}
	got, err := navigationURI(unsafe.Pointer(&args), args.vtbl.GetURI)
	if err != nil || got != "http://wails.localhost/中文#section" || calls != 1 {
		t.Fatalf("LPWSTR输出地址/解码错误：%q %v %d", got, err, calls)
	}
	var values []uintptr
	put := NewComProc(func(_ unsafe.Pointer, value uintptr) uintptr { values = append(values, value); return 0 })
	for _, value := range []bool{false, true, false, true} {
		if err := navigationSetBOOL(unsafe.Pointer(&args), put, value); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(values, []uintptr{0, 1, 0, 1}) {
		t.Fatalf("BOOL必须按值传0/1，而非Go bool地址：%v", values)
	}
}

func TestNavigationPolicyNativeEventInvokeCancelsFramesAndHandlesPopups(t *testing.T) {
	for _, kind := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			for _, allowed := range []bool{false, true} {
				calls, cancelled, handled := 0, []uintptr{}, []uintptr{}
				seen := ""
				callback := func(uri string) bool { seen = uri; return allowed }
				e := &Chromium{NavigationStartingCallback: callback, FrameNavigationStartingCallback: callback, NewWindowRequestedCallback: callback}
				h := &navigationHandler{vtbl: &navigationHandlerTable, owner: e, kind: kind}
				get := navigationTestURI(t, "https://example.invalid/target", &calls)
				args := navigationStartingArgs{vtbl: &navigationStartingArgsVtbl{GetURI: get, PutCancel: NewComProc(func(_ unsafe.Pointer, value uintptr) uintptr { cancelled = append(cancelled, value); return 0 })}}
				popup := newWindowArgs{vtbl: &newWindowArgsVtbl{GetURI: get, PutHandled: NewComProc(func(_ unsafe.Pointer, value uintptr) uintptr { handled = append(handled, value); return 0 })}}
				raw := unsafe.Pointer(&args)
				if kind == 2 {
					raw = unsafe.Pointer(&popup)
				}
				hr, _, _ := h.vtbl.Invoke.Call(uintptr(unsafe.Pointer(h)), 0, uintptr(raw))
				runtime.KeepAlive(args)
				runtime.KeepAlive(popup)
				if hr != 0 || calls != 1 || seen != "https://example.invalid/target" {
					t.Fatalf("真实native thunk未调用正确事件：%x %d %s", hr, calls, seen)
				}
				if allowed {
					if len(cancelled)+len(handled) != 0 {
						t.Fatal("允许分支不能写入否决字段")
					}
				} else if kind == 2 {
					if !reflect.DeepEqual(handled, []uintptr{1}) || len(cancelled) != 0 {
						t.Fatal("新窗口必须实际设置Handled=1")
					}
				} else if !reflect.DeepEqual(cancelled, []uintptr{1}) || len(handled) != 0 {
					t.Fatal("主/子frame必须实际设置Cancel=1")
				}
			}
		})
	}
}

func TestNavigationPolicyMissingURIReallyVetoesWithoutCallingAllow(t *testing.T) {
	for _, kind := range []int{0, 1, 2} {
		called := false
		callback := func(string) bool { called = true; return true }
		e := &Chromium{NavigationStartingCallback: callback, FrameNavigationStartingCallback: callback, NewWindowRequestedCallback: callback}
		h := &navigationHandler{owner: e, kind: kind}
		denied := uintptr(0)
		get := NewComProc(func(_ unsafe.Pointer, out **uint16) uintptr {
			if out == nil {
				t.Error("缺少输出地址")
			}
			return 0x80004005
		})
		put := NewComProc(func(_ unsafe.Pointer, value uintptr) uintptr { denied = value; return 0 })
		a := navigationStartingArgs{vtbl: &navigationStartingArgsVtbl{GetURI: get, PutCancel: put}}
		b := newWindowArgs{vtbl: &newWindowArgsVtbl{GetURI: get, PutHandled: put}}
		raw := unsafe.Pointer(&a)
		if kind == 2 {
			raw = unsafe.Pointer(&b)
		}
		if hr := navigationInvoke(h, nil, raw); hr != 0 || denied != 1 || called {
			t.Fatalf("URI读取失败不能只报错后默认放行：kind%d hr%x denied%d called%v", kind, hr, denied, called)
		}
	}
}

func navigationTestChromium(t *testing.T, failKind int) (*Chromium, *[3]*navigationHandler, *[]int) {
	t.Helper()
	handlers := &[3]*navigationHandler{}
	removed := &[]int{}
	vt := &iCoreWebView2Vtbl{}
	adds := []*ComProc{&vt.AddNavigationStarting, &vt.AddFrameNavigationStarting, &vt.AddNewWindowRequested}
	removes := []*ComProc{&vt.RemoveNavigationStarting, &vt.RemoveFrameNavigationStarting, &vt.RemoveNewWindowRequested}
	for kind := 0; kind < 3; kind++ {
		k := kind
		*adds[k] = NewComProc(func(_ *ICoreWebView2, h *navigationHandler, token *_EventRegistrationToken) uintptr {
			if k == failKind {
				return 0x80004005
			}
			handlers[k] = h
			token.value = int64(k + 71)
			h.vtbl.AddRef.Call(uintptr(unsafe.Pointer(h)))
			return 0
		})
		*removes[k] = NewComProc(func(_ *ICoreWebView2, token uintptr) uintptr {
			if token != uintptr(k+71) {
				t.Errorf("事件token按值传递错误：%d/%d", token, k+71)
			}
			*removed = append(*removed, k)
			h := handlers[k]
			if h == nil {
				t.Error("注销了未登记事件")
			} else {
				h.vtbl.Release.Call(uintptr(unsafe.Pointer(h)))
			}
			return 0
		})
	}
	return &Chromium{webview: &ICoreWebView2{vtbl: vt}, NavigationStartingCallback: func(string) bool { return false }, FrameNavigationStartingCallback: func(string) bool { return false }, NewWindowRequestedCallback: func(string) bool { return false }}, handlers, removed
}

func TestNavigationPolicyRegistrationLifetimeAndFailureRollback(t *testing.T) {
	for _, failKind := range []int{-1, 1} {
		t.Run(fmt.Sprint(failKind), func(t *testing.T) {
			e, handlers, removed := navigationTestChromium(t, failKind)
			err := e.registerNavigationPolicyHandlers()
			if failKind >= 0 {
				if err == nil || e.navigationEvents != nil || !reflect.DeepEqual(*removed, []int{0}) {
					t.Fatalf("部分注册失败必须注销已登记部分：%v %v", err, *removed)
				}
				for _, h := range handlers {
					if h != nil {
						if _, alive := navigationLiveHandlers.Load(h); alive {
							t.Fatal("失败注册残留Go owner引用")
						}
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for kind, h := range handlers {
				if h == nil || h.refs.Load() != 2 {
					t.Fatalf("未保持owner+COM双引用：%d", kind)
				}
				var object unsafe.Pointer
				iid := navigationHandlerIID
				if kind == 2 {
					iid = newWindowHandlerIID
				}
				hr, _, _ := h.vtbl.QueryInterface.Call(uintptr(unsafe.Pointer(h)), uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&object)))
				if hr != 0 || object != unsafe.Pointer(h) || h.refs.Load() != 3 {
					t.Fatal("QueryInterface必须返回正确对象并AddRef")
				}
				h.vtbl.Release.Call(uintptr(unsafe.Pointer(h)))
				unknown := windows.GUID{Data1: 123}
				object = unsafe.Pointer(h)
				hr, _, _ = h.vtbl.QueryInterface.Call(uintptr(unsafe.Pointer(h)), uintptr(unsafe.Pointer(&unknown)), uintptr(unsafe.Pointer(&object)))
				if uint32(hr) != 0x80004002 || object != nil {
					t.Fatal("未知IID必须E_NOINTERFACE且清空输出")
				}
			}
			runtime.GC()
			e.CloseNavigationPolicyHandlers()
			e.CloseNavigationPolicyHandlers()
			if !reflect.DeepEqual(*removed, []int{0, 1, 2}) || e.navigationEvents != nil {
				t.Fatalf("三类事件必须各注销一次：%v", *removed)
			}
			for _, h := range handlers {
				if h.refs.Load() != 0 {
					t.Fatal("注销后引用未释放")
				}
				if _, alive := navigationLiveHandlers.Load(h); alive {
					t.Fatal("正常注销残留全局Go引用")
				}
			}
		})
	}
	if err := (&Chromium{}).registerNavigationPolicyHandlers(); err != nil {
		t.Fatal("默认未启用不能注册原生事件")
	}
}

func TestNavigationPolicyVetoFailureExitsWithoutReturning(t *testing.T) {
	if path := os.Getenv("CODEX_SPACE_FAKE_COM_FATAL_RECORD"); path != "" {
		appendEvent := func(value string) {
			file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				panic(err)
			}
			_, _ = file.WriteString(value + "\n")
			_ = file.Close()
		}
		e := &Chromium{NavigationStartingCallback: func(string) bool { return false }, globalErrorCallback: func(error) { appendEvent("fatal") }}
		calls := 0
		a := navigationStartingArgs{vtbl: &navigationStartingArgsVtbl{GetURI: navigationTestURI(t, "https://example.invalid/", &calls), PutCancel: NewComProc(func(_ unsafe.Pointer, value uintptr) uintptr {
			appendEvent(fmt.Sprintf("cancel=%d", value))
			return 0x80004005
		})}}
		navigationInvoke(&navigationHandler{owner: e}, nil, unsafe.Pointer(&a))
		appendEvent("returned-default")
		return
	}
	path := filepath.Join(t.TempDir(), "fatal-events.txt")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestNavigationPolicyVetoFailureExitsWithoutReturning$")
	cmd.Env = append(os.Environ(), "CODEX_SPACE_FAKE_COM_FATAL_RECORD="+path)
	output, err := cmd.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 1 {
		t.Fatalf("既有fatal路径必须实际退出1：%v %s", err, output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "cancel=1\nfatal" {
		t.Fatalf("不能仅返回HRESULT，必须沿既有fatal退出，不回落默认动作：%s", data)
	}
}
