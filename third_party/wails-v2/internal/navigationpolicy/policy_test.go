package navigationpolicy

import "testing"

func TestExactOriginNavigationPolicy(t *testing.T) {
	for _, start := range []string{"http://wails.localhost/", "wails://wails/", "http://127.0.0.1:37001/"} {
		p := New(true, start)
		for _, suffix := range []string{"", "assets/index.js", "?mode=local#section", "中文/路径"} {
			if !p.Allow(start+suffix, false) {
				t.Fatalf("内部地址被拒：%s%s", start, suffix)
			}
		}
		if p.Allow(start, true) {
			t.Fatal("内部地址也不能自动创建新窗口")
		}
		for _, target := range []string{"https://example.invalid/", "http://wails.localhost.evil.invalid/", "http://wails.localhost@evil.invalid/", "http://evil.invalid@wails.localhost/", "http://wails.localhost:31001/", "file:///C:/private.txt", "javascript:alert(1)", "data:text/html,test", "about:blank", "//wails.localhost/", "wails://other/", "http://wails.localhost:/", "http://wails.localhost:0/", "http://wails.localhost:080/", "http://wails.localhost:65536/", "http://wails.localhost/%00", "http://wails.localhost/?bad=%0d", "http://wails.localhost/?bad=%GG", "http://wails.localhost\\@evil.invalid/", " http://wails.localhost/", "http://wails.localhost/\n", "http://wails.localhost./", "http://wails.localhost/%zz"} {
			if p.Allow(target, false) {
				t.Fatalf("畸形或外部地址被允许：起点%s 目标%s", start, target)
			}
		}
	}
	if !New(true, "http://wails.localhost/").Allow("HTTP://WAILS.LOCALHOST:80/#same", false) {
		t.Fatal("原生规范化的大小写/默认端口应保持同源")
	}
	if New(true, "invalid").Allow("http://wails.localhost/", false) {
		t.Fatal("启动来源无效必须拒绝")
	}
}

func TestOptInAndRedirectReevaluation(t *testing.T) {
	if !New(false, "invalid").Allow("https://example.invalid/", true) {
		t.Fatal("未启用时不得改变上游行为")
	}
	p := New(true, "http://wails.localhost/")
	if !p.Allow("http://wails.localhost/start", false) || p.Allow("https://example.invalid/redirect", false) || !p.Allow("http://wails.localhost/after", false) {
		t.Fatal("每次导航/重定向独立核对，不能继承前次允许")
	}
}

func TestResourcePolicyKeepsOnlyInternalOrPNGData(t *testing.T) {
	p := New(true, "http://wails.localhost/")
	if !p.AllowResource("http://wails.localhost/assets/index.js") || !p.AllowResource("data:image/png;base64,iVBORw0KGgo=") {
		t.Fatal("内置资源或现产品PNG数据不能被误挡")
	}
	for _, target := range []string{"https://example.invalid/p.png", "data:image/svg+xml,<svg/>", "data:text/html,test", "blob:http://wails.localhost/id", "file:///C:/private.txt"} {
		if p.AllowResource(target) {
			t.Fatalf("资源不应被允许：%s", target)
		}
	}
	if p.Allow("data:image/png;base64,iVBORw0KGgo=", false) {
		t.Fatal("媒体数据许可不能升级为导航许可")
	}
}
