package telemetry

import "time"

// EventType 表示观测事件类型。
type EventType string

const (
	// EventRouteDecision 表示路由主流程决策事件。
	EventRouteDecision   EventType = "route_decision"
	// EventRouteFallback 表示路由进入回退链的事件。
	EventRouteFallback   EventType = "route_fallback"
	// EventDispatchResult 表示最终派发结果事件。
	EventDispatchResult  EventType = "dispatch_result"
	// EventObjectiveResult 表示目标函数执行结果事件。
	EventObjectiveResult EventType = "objective_result"
)

// TelemetryEvent 表示一次观测事件。
type TelemetryEvent struct {
	Type       EventType
	RouteClass string
	Stage      string
	Outcome    string
	Reason     string
	Plugin     string
	DurationMs int64
	Timestamp  time.Time
}

// Sink 是观测事件消费接口。
type Sink interface {
	OnEvent(e TelemetryEvent)
}

// NoopSink 是默认空实现。
type NoopSink struct{}

// OnEvent 实现空行为。
func (NoopSink) OnEvent(_ TelemetryEvent) {}

// EmitSafe 安全发送事件，保证不会影响主流程。
func EmitSafe(s Sink, e TelemetryEvent) {
	if s == nil {
		return
	}
	defer func() {
		recover()
	}()
	s.OnEvent(e)
}
