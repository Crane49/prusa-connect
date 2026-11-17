package simulator

import (
	"strconv"
	"strings"
)

// toolSetterPrefix is the start of the verb name of every tool setter the
// profiles declare: SET_TOOL_NOZZLE_DIAMETER, SET_TOOL_HARDENED and
// SET_TOOL_HIGH_FLOW.
const toolSetterPrefix = "SET_TOOL_"

// toolSetterKwargs reads a tool setter the way the firmware does, and turns it
// into the argument names the profile declares for it.
//
// The firmware reads a tool setter as SET_VALUE with one key,
// "tools.<k>.<name>". k counts the tools the way the printer's own INFO keys
// them: from 0 on a printer that sends the tree shape and from 1 on one that
// sends the numbered shape. The profile declares the setter with a tool and the
// setter name, which is what verbForArguments matches on, so k is carried back
// to the head id the host sent, counted from 0.
//
// ok is false when the kwargs are not exactly one such key, and they then come
// back unchanged. A key whose k is below the printer's first tool is not one.
func toolSetterKwargs(kwargs map[string]any, tree bool) (out map[string]any, ok bool) {
	if len(kwargs) != 1 {
		return kwargs, false
	}

	first := 1
	if tree {
		first = 0
	}

	for key, value := range kwargs {
		parts := strings.Split(key, ".")
		if len(parts) != 3 || parts[0] != "tools" || parts[2] == "" {
			return kwargs, false
		}

		k, err := strconv.Atoi(parts[1])
		if err != nil || k < first || strconv.Itoa(k) != parts[1] {
			return kwargs, false
		}

		return map[string]any{"tool": float64(k - first), parts[2]: value}, true
	}

	return kwargs, false
}
