package diagnose

import "fmt"

func BuildDiagnosePrompt(userError string) string {
	facts := CollectFacts()
	factsStr := Format(facts)

	return fmt.Sprintf(`The user is reporting the following issue:
> %s

Below are automatically collected system facts. Use these as your starting point for diagnosis.
Do NOT re-run commands whose output is already provided below unless you need fresher data.

%s

Please diagnose this issue following the structured format: Summary, Root Cause, Risk Level, Evidence, Remediation, Rollback.`, userError, factsStr)
}
