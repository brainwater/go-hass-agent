// Copyright (c) 2023 Joshua Rich <joshua.rich@gmail.com>
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package linux

//go:generate stringer -type=batteryLevel -output batteryLevelsStrings.go -linecomment
const (
	batteryLevelUnknown batteryLevel = iota // Unknown
	batteryLevelNone                        // None
	_
	batteryLevelLow  // Low
	batteryLevelCrit // Critical
	_
	batteryLevelNorm // Normal
	batteryLevelHigh // High
	batteryLevelFull // Full
)

type batteryLevel uint32
