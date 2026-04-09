package diagnose

import "fmt"

type IssueClass string

const (
	IssueGeneral     IssueClass = "general"
	IssueDisk        IssueClass = "disk"
	IssueMemory      IssueClass = "memory"
	IssuePerformance IssueClass = "performance"
	IssueService     IssueClass = "service"
	IssueDNS         IssueClass = "dns"
	IssueNetwork     IssueClass = "network"
	IssueDocker      IssueClass = "docker"
	IssueBuild       IssueClass = "build"
	IssuePermission  IssueClass = "permission"
	IssuePort        IssueClass = "port"
	IssueSSL         IssueClass = "ssl"
	IssueGit         IssueClass = "git"
	IssueCron        IssueClass = "cron"
	IssuePackage     IssueClass = "package"
	IssueProcess     IssueClass = "process"
)

func BuildDiagnosePrompt(userError string) string {
	issueClass := ClassifyIssue(userError)
	facts := CollectFactsForIssue(issueClass)
	factsStr := Format(facts)

	return fmt.Sprintf(`The user is reporting the following issue:
> %s

Issue class: %s

Below are automatically collected system facts. Use these as your starting point for diagnosis.
Do NOT re-run commands whose output is already provided below unless you need fresher data.

%s

Please diagnose this issue following the structured format: Summary, Root Cause, Risk Level, Evidence, Remediation, Rollback.`, userError, issueClass, factsStr)
}
