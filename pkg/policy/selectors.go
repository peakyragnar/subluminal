package policy

import "strings"

func selectorsMatch(selectors PolicySelectors, target SelectorTarget) bool {
	if selectors.IsZero() {
		return true
	}
	if !selectorListMatch(selectors.Env, target.Env) {
		return false
	}
	if !selectorListMatch(selectors.AgentID, target.AgentID) {
		return false
	}
	if !selectorListMatch(selectors.Client, target.Client) {
		return false
	}
	if selectors.Workload != nil {
		if !workloadMatch(*selectors.Workload, target.Workload) {
			return false
		}
	}
	return true
}

func selectorListMatch(values []string, actual string) bool {
	if len(values) == 0 {
		return true
	}
	if strings.TrimSpace(actual) == "" {
		return false
	}
	actual = strings.ToLower(strings.TrimSpace(actual))
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == actual {
			return true
		}
	}
	return false
}

func workloadMatch(selector WorkloadSelector, target WorkloadContext) bool {
	if !selectorListMatch(selector.Namespace, target.Namespace) {
		return false
	}
	if !selectorListMatch(selector.ServiceAccount, target.ServiceAccount) {
		return false
	}
	if !selectorListMatch(selector.Repo, target.Repo) {
		return false
	}
	if !selectorListMatch(selector.Branch, target.Branch) {
		return false
	}
	if len(selector.Labels) == 0 {
		return true
	}
	if len(target.Labels) == 0 {
		return false
	}
	for key, value := range selector.Labels {
		if targetValue, ok := target.Labels[key]; !ok || targetValue != value {
			return false
		}
	}
	return true
}
