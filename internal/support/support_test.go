package support

import (
	"strings"
	"testing"
	"time"
)

func d(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

// steady is a project that ships a major a year and patches the previous
// one for about two months after.
func steady() []Release {
	return []Release{
		{"1.0.0", d("2020-01-01")}, {"1.1.0", d("2020-06-01")},
		{"2.0.0", d("2021-01-01")}, {"1.1.1", d("2021-02-15")},
		{"2.1.0", d("2021-06-01")},
		{"3.0.0", d("2022-01-01")}, {"2.1.1", d("2022-03-01")},
		{"3.1.0", d("2022-06-01")},
		{"4.0.0", d("2023-01-01")}, {"3.1.1", d("2023-02-01")},
		{"4.1.0", d("2023-03-01")}, {"4.2.0", d("2023-05-01")},
		{"5.0.0", d("2024-01-01")}, {"4.2.1", d("2024-02-01")},
		{"5.1.0", d("2024-03-01")}, {"5.2.0", d("2024-04-01")},
		{"5.3.0", d("2024-05-01")}, {"5.4.0", d("2024-06-01")},
	}
}

func TestInferSupportWindow(t *testing.T) {
	now := d("2024-07-01")
	cases := []struct {
		version          string
		ended, confident bool
	}{
		{"5.2.0", false, false}, // newest line
		{"4.2.1", true, false},  // ended, but only 5 months past its successor
		{"2.1.1", true, true},   // long gone
		{"9.0.0", false, false}, // unknown line
		{"main", false, false},  // not a version
	}
	for _, c := range cases {
		v := Infer(steady(), c.version, now)
		if v.Ended != c.ended || v.Confident != c.confident {
			t.Errorf("%s: ended=%v confident=%v (%s), want %v %v", c.version, v.Ended, v.Confident, v.Reason, c.ended, c.confident)
		}
	}
}

func TestInferLongLivedLines(t *testing.T) {
	// A project that patches old majors for years: line 4 is still inside
	// the habit, so it is not flagged.
	h := []Release{
		{"1.0.0", d("2018-01-01")}, {"2.0.0", d("2019-01-01")}, {"1.9.0", d("2021-01-01")},
		{"3.0.0", d("2020-01-01")}, {"2.9.0", d("2022-01-01")},
		{"4.0.0", d("2021-06-01")}, {"3.9.0", d("2023-01-01")},
		{"5.0.0", d("2023-06-01")}, {"4.5.0", d("2024-01-01")},
		{"5.1.0", d("2024-05-01")},
	}
	if v := Infer(h, "4.5.0", d("2024-06-01")); v.Ended {
		t.Errorf("4.5.0 flagged: %s", v.Reason)
	}
}

func TestInferZeroVersionLines(t *testing.T) {
	h := []Release{
		{"0.1.0", d("2022-01-01")}, {"0.2.0", d("2022-04-01")},
		{"0.3.0", d("2022-07-01")}, {"0.4.0", d("2022-10-01")},
		{"0.5.0", d("2023-01-01")}, {"0.5.1", d("2024-05-01")},
	}
	v := Infer(h, "0.2.0", d("2024-06-01"))
	if !v.Ended || !v.Confident || v.Line != "0.2" {
		t.Errorf("0.2.0 = %+v", v)
	}
}

func TestInferAbandoned(t *testing.T) {
	var h []Release
	start := d("2018-01-01")
	for i := 0; i < 10; i++ {
		h = append(h, Release{"1." + string(rune('0'+i)) + ".0", start.AddDate(0, i, 0)})
	}
	v := Infer(h, "1.9.0", d("2021-01-01"))
	if !v.Ended || !v.Confident || !strings.Contains(v.Reason, "no release in") {
		t.Errorf("abandoned = %+v", v)
	}
	if v := Infer(h[:3], "1.2.0", d("2021-01-01")); !v.Ended || v.Confident {
		t.Errorf("short history = %+v, want ended but not confident", v)
	}
	if v := Infer(h, "1.9.0", d("2018-12-01")); v.Ended {
		t.Errorf("recent = %+v", v)
	}
}
