package lu

import "context"

//go:generate stringer -type=EventType

type OnEvent func(context.Context, Event)

type EventType int

const (
	Unknown        EventType = iota
	AppStartup               // First event, emitted right at the start
	PreHookStart             // Emitted just before running each Hook.Start
	PostHookStart            // Emitted just after completing a Hook.Start
	AppRunning               // Emitted after running every startup Hook
	ProcessStart             // Emitted before starting to run a Process
	ProcessEnd               // Emitted when a Process terminates
	AppTerminating           // Emitted when the application starts termination
	PreHookStop              // Emitted before running each Hook.Stop
	PostHookStop             // Emitted after running each Hook.Stop
	AppTerminated            // Emitted before calling os.Exit
)

type Event struct {
	Type EventType
	Name string
}
