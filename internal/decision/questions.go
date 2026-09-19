package decision

// AssessQuestions returns the canonical first-pass questions.
func AssessQuestions() map[string]Question {
	return map[string]Question{
		"primary_dimension":     {Type: QChoice, Instructions: "Which resource is most likely constrained on this machine?", Criteria: map[string]string{"cpu": "CPU time is the scarce resource", "memory": "Memory capacity or reclaim is the scarce resource", "io": "Block device throughput or latency is the scarce resource", "network": "Network throughput, sockets or retransmits are the problem", "scheduling": "CPU is available but threads are not being scheduled", "limits": "A configured limit is being hit", "none": "Nothing is constrained; the machine is behaving normally"}},
		"cpu_constrained":       {Type: QNoul, Instructions: "Is CPU a real constraint here, rather than healthy busy work?", Criteria: map[string]string{"true": "Constrained", "false": "Not constrained"}},
		"memory_constrained":    {Type: QNoul, Instructions: "Is memory capacity or reclaim causing a problem?", Criteria: map[string]string{"true": "Constrained", "false": "Not constrained"}},
		"io_constrained":        {Type: QNoul, Instructions: "Is block I/O causing a problem?", Criteria: map[string]string{"true": "Constrained", "false": "Not constrained"}},
		"explained_by_workload": {Type: QNoul, Instructions: "Is this load fully explained by normal workload volume rather than a fault?", Criteria: map[string]string{"true": "Explained", "false": "Not explained"}},
		"severity":              {Type: QScore, Instructions: "How abnormal is this machine's state?", Levels: []string{"normal", "mildly unusual", "suspicious", "strongly abnormal", "critical"}},
	}
}

// NextQuestions builds a bounded question set over the legal capability IDs.
func NextQuestions(legal []string) map[string]Question {
	c := map[string]string{"stop": "The evidence already explains the anomaly or deeper inspection will not help"}
	for _, x := range legal {
		c[x] = "Legal next investigation capability: " + x
	}
	return map[string]Question{"next_capability": {Type: QChoice, Instructions: "Which investigation step best explains the anomaly?", Criteria: c}, "explains_anomaly": {Type: QNoul, Instructions: "Does the collected evidence explain the anomaly?", Criteria: map[string]string{"true": "Yes", "false": "No"}}, "deeper_warranted": {Type: QNoul, Instructions: "Would one more level materially increase confidence?", Criteria: map[string]string{"true": "Yes", "false": "No"}}}
}
