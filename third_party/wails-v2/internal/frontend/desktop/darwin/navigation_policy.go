//go:build darwin

package darwin

/*
#include <stdlib.h>
*/
import "C"

import (
	"github.com/wailsapp/wails/v2/internal/navigationpolicy"
	"sync/atomic"
)

var activeNavigationPolicy atomic.Pointer[navigationpolicy.Policy]

func configureNavigationPolicy(enabled bool, startURL string) {
	p := navigationpolicy.New(enabled, startURL)
	activeNavigationPolicy.Store(&p)
}

//export allowNavigationRequest
func allowNavigationRequest(uri *C.char, newWindow C.int) C.int {
	p := activeNavigationPolicy.Load()
	if p != nil && p.Allow(C.GoString(uri), newWindow != 0) {
		return 1
	}
	return 0
}
