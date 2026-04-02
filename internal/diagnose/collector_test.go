package diagnose

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyIssue(t *testing.T) {
	tests := []struct {
		input string
		want  IssueClass
	}{
		{input: "disk space is full", want: IssueDisk},
		{input: "memory usage is too high", want: IssueMemory},
		{input: "the server is slow and sluggish", want: IssuePerformance},
		{input: "dns resolution is broken", want: IssueDNS},
		{input: "network connectivity keeps dropping", want: IssueNetwork},
		{input: "nginx won't start", want: IssueService},
		{input: "general system health check", want: IssueGeneral},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, ClassifyIssue(tt.input))
		})
	}
}

func TestPlanFactsForIssue(t *testing.T) {
	diskPlan := planFactsForIssue(IssueDisk)
	assert.Len(t, diskPlan, 3)
	assert.Equal(t, "Disk (df -h)", diskPlan[0].Title)

	servicePlan := planFactsForIssue(IssueService)
	assert.Len(t, servicePlan, 3)
	assert.Equal(t, "Failed Services", servicePlan[1].Title)

	generalPlan := planFactsForIssue(IssueGeneral)
	assert.GreaterOrEqual(t, len(generalPlan), 5)
}

func TestExtractServiceName(t *testing.T) {
	assert.Equal(t, "nginx", ExtractServiceName("nginx won't start"))
	assert.Equal(t, "postgres", ExtractServiceName("postgres keeps crashing"))
	assert.Equal(t, "", ExtractServiceName("the server is slow"))
}
