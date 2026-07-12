package limitpolicy

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	policyDecisionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "unirouter_limit_policy_decisions_total",
		Help: "Distributed limit-policy decisions with bounded labels.",
	}, []string{"mode", "decision", "reason"})
	policyTerminalTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "unirouter_limit_policy_terminal_total",
		Help: "Distributed limit-policy terminal outcomes with bounded labels.",
	}, []string{"terminal", "outcome"})
)

func recordDecisionMetric(decision Decision) {
	policyDecisionsTotal.WithLabelValues(decision.Mode, decision.Decision, decision.Reason).Inc()
}

func recordTerminalMetric(terminal, outcome string) {
	policyTerminalTotal.WithLabelValues(terminal, outcome).Inc()
}
