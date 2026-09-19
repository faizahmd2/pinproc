package logs

import (
	"github.com/joshdurbin/drain3"
)

const drainThreshold = 20

type DrainResult struct {
	Used      bool
	Templates []LogTemplate
}

type LogTemplate struct {
	Template string
	Count    int
}

func Compress(lines []string) (DrainResult, error) {
	if len(lines) < drainThreshold {
		return DrainResult{
			Used: false,
		}, nil
	}

	engine, err := drain3.New()
	if err != nil {
		return DrainResult{}, err
	}

	for _, line := range lines {
		engine.AddLogMessage(line)
	}

	result := DrainResult{
		Used: true,
	}

	for _, cluster := range engine.Clusters() {
		result.Templates = append(
			result.Templates,
			LogTemplate{
				Template: cluster.GetTemplate(),
				Count:    cluster.Size,
			},
		)
	}

	return result, nil
}
