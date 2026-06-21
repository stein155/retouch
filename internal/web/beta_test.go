package web

import "testing"

func TestBetaPR(t *testing.T) {
	cases := []struct {
		tag    string
		wantN  int
		wantOK bool
	}{
		{"beta-pr-12", 12, true},
		{"beta-pr-1", 1, true},
		{"beta-pr-007", 7, true},
		{"v1.2.3", 0, false},
		{"beta-pr-", 0, false},
		{"beta-pr-x", 0, false},
		{"beta-pr-12-abc", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		n, ok := betaPR(c.tag)
		if ok != c.wantOK || n != c.wantN {
			t.Errorf("betaPR(%q) = (%d, %v), want (%d, %v)", c.tag, n, ok, c.wantN, c.wantOK)
		}
	}
}
