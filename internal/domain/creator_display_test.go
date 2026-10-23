package domain

import "testing"

func TestCreatorDisplayName(t *testing.T) {
	for raw, want := range map[string]string{
		"  By Ada; Bob；Carol、Dana | Eve,;  ": "Ada · Bob · Carol · Dana · Eve",
		"BY: Ada / Bob & Carol;":             "Ada · Bob · Carol",
		"Byron Smith":                        "Byron Smith",
		"Smith, John":                        "Smith, John",
		"Smith and Sons":                     "Smith and Sons",
		"A/B":                                "A/B",
		"未知作者":                               "未知作者",
		"":                                   "",
		" ;；, ":                              "",
	} {
		if got := CreatorDisplayName(raw); got != want {
			t.Errorf("%q => %q, want %q", raw, got, want)
		}
	}
}
