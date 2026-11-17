package compose

import "strings"

// withoutSafe returns args without the -safe flag, in both its dash forms and
// with or without a value. The flag package reads a bool flag only as -safe or
// -safe=value, never as -safe value, so no following argument belongs to it.
func withoutSafe(args []string) []string {
	kept := make([]string, 0, len(args))

	for _, arg := range args {
		switch {
		case arg == "-safe", arg == "--safe":
		case strings.HasPrefix(arg, "-safe="), strings.HasPrefix(arg, "--safe="):
		default:
			kept = append(kept, arg)
		}
	}

	return kept
}
