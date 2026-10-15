package account

import "testing"

func TestPlanTier(t *testing.T) {
	cases := map[string]string{"SuperGrokPro": "heavy", "SuperGrok Heavy": "heavy", "supergrok_heavy": "heavy", "SuperGrok": "supergrok", "supergrok_lite": "lite", "Free": "free", "X Premium+": "raw", "": ""}
	for in, want := range cases {
		if got := planTier(in); got != want {
			t.Fatalf("planTier(%q)=%q want %q", in, got, want)
		}
	}
}

func TestConsoleNoQuota(t *testing.T) {
	v := accountResponse{Provider: "grok_console", QuotaWindows: []quotaWindowResponse{{Mode: "console", Total: 0}, {Mode: "console_image", Total: 0}}}
	if !consoleNoQuota(v) {
		t.Fatal("limit=0 console should be noQuota")
	}
	v.QuotaWindows[0].Total = 10
	if consoleNoQuota(v) {
		t.Fatal("limit>0 console is not noQuota")
	}
	if consoleNoQuota(accountResponse{Provider: "grok_build"}) || consoleNoQuota(accountResponse{Provider: "grok_console"}) {
		t.Fatal("non-console / unsynced must not be noQuota")
	}
}
