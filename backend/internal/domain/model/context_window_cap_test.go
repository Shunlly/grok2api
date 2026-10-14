package model

import "testing"

func TestResolveContextWindowCap(t *testing.T) {
	cases := []struct {
		in   []int
		want int
	}{{nil, 0}, {[]int{0, 256000}, 0}, {[]int{256000}, 256000}, {[]int{256000, 500000}, 500000}}
	for _, c := range cases {
		if got := ResolveContextWindowCap(c.in); got != c.want {
			t.Fatalf("%v => %d, want %d", c.in, got, c.want)
		}
	}
}

func TestApplyContextWindowCapPrefersSelectableSize(t *testing.T) {
	p := UpstreamModelProfile{ContextWindow: 256000, ContextWindows: []int{256000, 500000}}
	if got := p.ApplyContextWindowCap(500000, 0); got != 500000 {
		t.Fatalf("no cap = %d", got)
	}
	if got := p.ApplyContextWindowCap(500000, 300000); got != 256000 {
		t.Fatalf("cap 300k = %d", got)
	}
	if got := p.ApplyContextWindowCap(500000, 100000); got != 100000 {
		t.Fatalf("cap 100k = %d", got)
	}
	if got := p.SelectableContextWindows(); len(got) != 2 || got[0] != 256000 || got[1] != 500000 {
		t.Fatalf("selectable = %v", got)
	}
}
