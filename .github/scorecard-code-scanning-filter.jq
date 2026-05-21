# Keep the full OpenSSF Scorecard SARIF as an Actions artifact, but do not
# upload repository-governance checks as code-scanning alerts. These findings
# depend on external service state, repository age, or branch/ruleset policy
# rather than vulnerable source code.
def repository_governance_checks:
  [
    "BranchProtectionID",
    "CIIBestPracticesID",
    "CodeReviewID",
    "MaintainedID"
  ];

(.runs[].results) |= map(
  select((.ruleId // "") as $id | (repository_governance_checks | index($id) | not))
)
