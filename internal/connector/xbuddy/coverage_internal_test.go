package xbuddy

import "testing"

// The progress a host reads is the distinct bytes a device has been served. A
// Buddy board asks for the end of a G-code file first and the rest after, and
// asks for a range again after a dropped connection, so neither the furthest
// byte nor the sum of what was written says how far a file has got.
func TestCoverageCountsEachByteOfTheFileOnce(t *testing.T) {
	t.Parallel()

	const size = 1000

	cases := []struct {
		name  string
		spans [][2]int64
		want  []int64
	}{
		{
			name:  "read in order",
			spans: [][2]int64{{0, 400}, {400, 800}, {800, size}},
			want:  []int64{400, 800, size},
		},
		{
			name:  "the end first, as a Buddy board reads a G-code file",
			spans: [][2]int64{{950, size}, {0, 300}, {300, 950}},
			want:  []int64{50, 350, size},
		},
		{
			name:  "a range asked for again after a dropped connection",
			spans: [][2]int64{{0, 500}, {200, 500}, {500, size}},
			want:  []int64{500, 500, size},
		},
		{
			name:  "ranges out of order with a gap between",
			spans: [][2]int64{{600, 700}, {100, 200}, {150, 650}},
			want:  []int64{100, 200, 600},
		},
		{
			name:  "an empty range",
			spans: [][2]int64{{0, 0}, {10, 10}},
			want:  []int64{0, 0},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var covered coverage

			for i, span := range testCase.spans {
				if got := covered.add(span[0], span[1]); got != testCase.want[i] {
					t.Errorf("after %v the coverage is %d, want %d", span, got, testCase.want[i])
				}
			}
		})
	}
}
