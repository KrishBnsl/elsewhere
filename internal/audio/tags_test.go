package audio

import "testing"

func TestTagNumberAndGain(t *testing.T) {
	for in, want := range map[string]int{"3": 3, " 7/12 ": 7, "": 0, "x": 0, "-4": 0} {
		if got := tagNumber(in); got != want {
			t.Errorf("tagNumber(%q) = %d, want %d", in, got, want)
		}
	}
	if g := tagGain("-6.5 dB"); g == nil || *g != -6.5 {
		t.Errorf("tagGain with unit = %v", g)
	}
	for _, bad := range []string{"", "loud", "NaN", "Inf"} {
		if g := tagGain(bad); g != nil {
			t.Errorf("tagGain(%q) should be nil, got %v", bad, *g)
		}
	}
}
