package tunnel

import "github.com/prometheus/client_golang/prometheus"

// Reasons an authenticated Agent is denied, the label values of AuthorizationDenials.
const (
	// DeniedDeadlinePassed is a session grant at or after the Workflow's execution deadline.
	DeniedDeadlinePassed = "deadline_passed"
	// DeniedNoSession is a request on a session key that has no session.
	DeniedNoSession = "no_session"
	// DeniedSessionEnded is a request on a session that has ended.
	DeniedSessionEnded = "session_ended"
	// DeniedWrongService is a session key calling something other than the WorkflowService.
	DeniedWrongService = "wrong_service"
	// DeniedWrongAgent is a session request that names another Agent.
	DeniedWrongAgent = "wrong_agent"
	// DeniedWrongWorkflow is a session request for another Workflow.
	DeniedWrongWorkflow = "wrong_workflow"
	// DeniedWrongTask is a report on a session for an Action that isn't its Agent's.
	DeniedWrongTask = "wrong_task"
)

// AuthorizationDenials counts requests from authenticated Agents that their
// authorization doesn't cover. An Agent with nothing to run isn't denied, so
// WAITING isn't counted.
var AuthorizationDenials = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "tink_server_agent_authorization_denials_total",
	Help: "Requests from authenticated Agents outside what their session or Workflow authorizes, by reason.",
}, []string{"reason"})

func denied(reason string) {
	AuthorizationDenials.WithLabelValues(reason).Inc()
}
