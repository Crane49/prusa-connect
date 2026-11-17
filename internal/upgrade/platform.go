package upgrade

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// Platform names this build's operating system and processor the way the
// update server lists builds, as <os>_<arch>. Go calls every 32-bit ARM
// build arm; the update server tells ARMv7 from the ARMv6 hard-float build a
// Raspberry Pi Zero runs, so arm becomes armv7, or armhf for GOARM=6.
func Platform() string {
	return platformFor(runtime.GOOS, runtime.GOARCH, buildSetting("GOARM"))
}

func platformFor(goos, goarch, goarm string) string {
	if goarch == "arm" {
		goarch = "armv7"
		if version, _, _ := strings.Cut(goarm, ","); version == "6" || version == "5" {
			goarch = "armhf"
		}
	}

	return goos + "_" + goarch
}

func buildSetting(key string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == key {
			return setting.Value
		}
	}

	return ""
}

// armName is armv7 or armhf, the name a 32-bit ARM release file carries.
func armName() string {
	_, arch, _ := strings.Cut(platformFor("linux", "arm", buildSetting("GOARM")), "_")

	return arch
}
