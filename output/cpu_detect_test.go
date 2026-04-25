package output

import "testing"

func TestARMCoreName(t *testing.T) {
	cases := []struct {
		impl, part string
		want       string
	}{
		{"0x41", "0xd05", "Cortex-A55"},
		{"0x41", "0xd41", "Cortex-A78"},
		{"0x41", "0xd4f", "Neoverse-V2"},
		{"0x41", "0xffff", "ARM 0xffff"}, // unknown ARM part
		{"0x51", "0xabcd", "0x51:0xabcd"}, // non-ARM implementer (Qualcomm)
	}
	for _, c := range cases {
		got := armCoreName(c.impl, c.part)
		if got != c.want {
			t.Errorf("armCoreName(%q, %q) = %q, want %q", c.impl, c.part, got, c.want)
		}
	}
}
