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

func TestExtractServiceName_Expanded(t *testing.T) {
	assert.Equal(t, "caddy", ExtractServiceName("caddy keeps restarting"))
	assert.Equal(t, "grafana", ExtractServiceName("grafana dashboard won't load"))
	assert.Equal(t, "haproxy", ExtractServiceName("haproxy is down"))
	assert.Equal(t, "elasticsearch", ExtractServiceName("elasticsearch cluster red"))
	assert.Equal(t, "traefik", ExtractServiceName("traefik proxy not routing"))
	assert.Equal(t, "prometheus", ExtractServiceName("prometheus scrape failing"))
	assert.Equal(t, "rabbitmq", ExtractServiceName("rabbitmq queue stuck"))
	assert.Equal(t, "consul", ExtractServiceName("consul agent not joining"))
}

func TestExtractServiceName_Fallback(t *testing.T) {
	// Unknown service name matched by fallback pattern
	assert.Equal(t, "myapp", ExtractServiceName("myapp service won't start"))
	assert.Equal(t, "foo-bar", ExtractServiceName("foo-bar daemon keeps crashing"))
	assert.Equal(t, "my-svc", ExtractServiceName("my-svc failed to start"))
	// False positives should be rejected
	assert.Equal(t, "", ExtractServiceName("the service is slow"))
	assert.Equal(t, "", ExtractServiceName("my service keeps failing"))
}
