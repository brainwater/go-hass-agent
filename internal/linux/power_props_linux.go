// Code generated by "stringer -type=powerProp -output power_props_linux.go"; DO NOT EDIT.

package linux

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[profile-3]
}

const _powerProp_name = "profile"

var _powerProp_index = [...]uint8{0, 7}

func (i powerProp) String() string {
	i -= 3
	if i < 0 || i >= powerProp(len(_powerProp_index)-1) {
		return "powerProp(" + strconv.FormatInt(int64(i+3), 10) + ")"
	}
	return _powerProp_name[_powerProp_index[i]:_powerProp_index[i+1]]
}
