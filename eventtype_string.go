// Code generated by "stringer -type=EventType"; DO NOT EDIT.

package lu

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[Unknown-0]
	_ = x[AppStartup-1]
	_ = x[PreHookStart-2]
	_ = x[PostHookStart-3]
	_ = x[AppRunning-4]
	_ = x[ProcessStart-5]
	_ = x[ProcessEnd-6]
	_ = x[AppTerminating-7]
	_ = x[PreHookStop-8]
	_ = x[PostHookStop-9]
	_ = x[AppTerminated-10]
}

const _EventType_name = "UnknownAppStartupPreHookStartPostHookStartAppRunningProcessStartProcessEndAppTerminatingPreHookStopPostHookStopAppTerminated"

var _EventType_index = [...]uint8{0, 7, 17, 29, 42, 52, 64, 74, 88, 99, 111, 124}

func (i EventType) String() string {
	if i < 0 || i >= EventType(len(_EventType_index)-1) {
		return "EventType(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _EventType_name[_EventType_index[i]:_EventType_index[i+1]]
}