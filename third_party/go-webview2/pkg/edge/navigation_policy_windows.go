//go:build windows

package edge

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 仅使用基础ICoreWebView2既有槽位，不转换为未经核对的高版本接口。
type navigationStartingArgsVtbl struct {
	_IUnknownVtbl
	GetURI, GetIsUserInitiated, GetIsRedirected, GetRequestHeaders, GetCancel, PutCancel, GetNavigationID ComProc
}
type navigationStartingArgs struct{ vtbl *navigationStartingArgsVtbl }
type newWindowArgsVtbl struct {
	_IUnknownVtbl
	GetURI, PutNewWindow, GetNewWindow, PutHandled, GetHandled, GetIsUserInitiated, GetDeferral, GetWindowFeatures ComProc
}
type newWindowArgs struct{ vtbl *newWindowArgsVtbl }

func navigationHRESULT(hr uintptr) error {
	if int32(hr) < 0 {
		return windows.Errno(hr)
	}
	return nil
}

// LPWSTR*必须传输出变量的地址；返回的字符串由COM分配，读取后立即释放。
func navigationURI(this unsafe.Pointer, get ComProc) (string, error) {
	var value *uint16
	hr, _, _ := get.Call(uintptr(this), uintptr(unsafe.Pointer(&value)))
	if err := navigationHRESULT(hr); err != nil {
		return "", err
	}
	if value == nil {
		return "", fmt.Errorf("navigation URI missing")
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(value))
	return windows.UTF16PtrToString(value), nil
}

func navigationSetBOOL(this unsafe.Pointer, put ComProc, value bool) error {
	var native uintptr
	if value {
		native = 1
	}
	hr, _, _ := put.Call(uintptr(this), native)
	return navigationHRESULT(hr)
}

type navigationHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}
type navigationHandler struct {
	vtbl  *navigationHandlerVtbl
	refs  atomic.Uint32
	owner *Chromium
	kind  int
}

var navigationLiveHandlers sync.Map
var navigationIUnknown = windows.GUID{Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
var navigationHandlerIID = windows.GUID{Data1: 0x9adbe429, Data2: 0xf36d, Data3: 0x432b, Data4: [8]byte{0x9d, 0xdc, 0xf8, 0x88, 0x1f, 0xbd, 0x76, 0xe3}}
var newWindowHandlerIID = windows.GUID{Data1: 0xd4c185fe, Data2: 0xc81c, Data3: 0x4989, Data4: [8]byte{0x97, 0xaf, 0x2d, 0x3f, 0xa7, 0xab, 0x56, 0x51}}

func navigationQueryInterface(h *navigationHandler, iid *windows.GUID, out *unsafe.Pointer) uintptr {
	if out == nil {
		return uintptr(0x80004003)
	}
	*out = nil
	expected := navigationHandlerIID
	if h.kind == 2 {
		expected = newWindowHandlerIID
	}
	if iid == nil || (*iid != navigationIUnknown && *iid != expected) {
		return uintptr(0x80004002)
	}
	*out = unsafe.Pointer(h)
	h.refs.Add(1)
	return 0
}
func navigationAddRef(h *navigationHandler) uintptr { return uintptr(h.refs.Add(1)) }
func navigationRelease(h *navigationHandler) uintptr {
	count := h.refs.Add(^uint32(0))
	if count == 0 {
		navigationLiveHandlers.Delete(h)
	}
	return uintptr(count)
}

func navigationInvoke(h *navigationHandler, _ *ICoreWebView2, raw unsafe.Pointer) uintptr {
	if raw == nil {
		h.owner.errorCallback(fmt.Errorf("navigation event missing"))
		return uintptr(0x80004003)
	}
	var allowed bool
	var err error
	if h.kind == 2 {
		args := (*newWindowArgs)(raw)
		uri, readErr := navigationURI(raw, args.vtbl.GetURI)
		allowed = readErr == nil && h.owner.NewWindowRequestedCallback != nil && h.owner.NewWindowRequestedCallback(uri)
		if !allowed {
			err = navigationSetBOOL(raw, args.vtbl.PutHandled, true)
		}
	} else {
		args := (*navigationStartingArgs)(raw)
		uri, readErr := navigationURI(raw, args.vtbl.GetURI)
		callback := h.owner.NavigationStartingCallback
		if h.kind == 1 {
			callback = h.owner.FrameNavigationStartingCallback
		}
		allowed = readErr == nil && callback != nil && callback(uri)
		if !allowed {
			err = navigationSetBOOL(raw, args.vtbl.PutCancel, true)
		}
	}
	if err != nil {
		h.owner.errorCallback(fmt.Errorf("native navigation veto failed: %w", err))
		return uintptr(0x80004005)
	}
	return 0
}

var navigationHandlerTable = navigationHandlerVtbl{_IUnknownVtbl{NewComProc(navigationQueryInterface), NewComProc(navigationAddRef), NewComProc(navigationRelease)}, NewComProc(navigationInvoke)}

type navigationRegistration struct {
	webview    *ICoreWebView2
	handlers   [3]*navigationHandler
	tokens     [3]_EventRegistrationToken
	registered [3]bool
}

func (e *Chromium) registerNavigationPolicyHandlers() error {
	if e.NavigationStartingCallback == nil && e.FrameNavigationStartingCallback == nil && e.NewWindowRequestedCallback == nil {
		return nil
	}
	r := &navigationRegistration{webview: e.webview}
	e.navigationEvents = r
	callbacks := [3]bool{e.NavigationStartingCallback != nil, e.FrameNavigationStartingCallback != nil, e.NewWindowRequestedCallback != nil}
	slots := [3]ComProc{e.webview.vtbl.AddNavigationStarting, e.webview.vtbl.AddFrameNavigationStarting, e.webview.vtbl.AddNewWindowRequested}
	for kind, enabled := range callbacks {
		if !enabled {
			continue
		}
		h := &navigationHandler{vtbl: &navigationHandlerTable, owner: e, kind: kind}
		h.refs.Store(1)
		navigationLiveHandlers.Store(h, struct{}{})
		r.handlers[kind] = h
		hr, _, _ := slots[kind].Call(uintptr(unsafe.Pointer(e.webview)), uintptr(unsafe.Pointer(h)), uintptr(unsafe.Pointer(&r.tokens[kind])))
		if err := navigationHRESULT(hr); err != nil {
			e.CloseNavigationPolicyHandlers()
			return err
		}
		r.registered[kind] = true
	}
	runtime.KeepAlive(r)
	return nil
}

// 仅在WebView所属线程注销事件；失败时仍由COM引用保持回调存活，不产生悬空指针。
func (e *Chromium) CloseNavigationPolicyHandlers() {
	r := e.navigationEvents
	if r == nil {
		return
	}
	e.navigationEvents = nil
	slots := [3]ComProc{r.webview.vtbl.RemoveNavigationStarting, r.webview.vtbl.RemoveFrameNavigationStarting, r.webview.vtbl.RemoveNewWindowRequested}
	for kind, h := range r.handlers {
		if h == nil {
			continue
		}
		if r.registered[kind] {
			value := uint64(r.tokens[kind].value)
			if unsafe.Sizeof(uintptr(0)) == 4 {
				slots[kind].Call(uintptr(unsafe.Pointer(r.webview)), uintptr(uint32(value)), uintptr(uint32(value>>32)))
			} else {
				slots[kind].Call(uintptr(unsafe.Pointer(r.webview)), uintptr(value))
			}
		}
		navigationRelease(h)
	}
	runtime.KeepAlive(r)
}
