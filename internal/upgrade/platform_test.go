package upgrade

import "testing"

func TestPlatformNamesThirtyTwoBitARMTheWayTheUpdateServerDoes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ goos, goarch, goarm, want string }{
		{"linux", "amd64", "", "linux_amd64"},
		{"linux", "arm64", "", "linux_arm64"},
		{"linux", "arm", "7", "linux_armv7"},
		{"linux", "arm", "", "linux_armv7"},
		{"linux", "arm", "6", "linux_armhf"},
		{"linux", "arm", "6,hardfloat", "linux_armhf"},
		{"darwin", "arm64", "", "darwin_arm64"},
	} {
		if got := platformFor(tc.goos, tc.goarch, tc.goarm); got != tc.want {
			t.Errorf("%s/%s GOARM=%q is %q, want %q", tc.goos, tc.goarch, tc.goarm, got, tc.want)
		}
	}
}
