# Renovate runbook

One-time setup for the Renovate workflow. What Renovate does is in the [README](../../README.md#dependency-updates).

1. Create a PAT. Fine-grained: this repo, with Contents, Pull requests, Workflows and Issues set to read-write.
   Classic: `repo` and `workflow`.
2. Add it as the repo secret `RENOVATE_TOKEN`. The built-in `GITHUB_TOKEN` lacks the scope, and PRs it opens do
   not start workflows.
3. Run the Renovate workflow by hand. It fills the dependency dashboard issue and opens the first PRs. One of
   them pins every action and base image to a digest.
4. Require the `go / go` and `renovate-config` checks on `main`, with no required reviews. Renovate cannot
   approve its own PR, so a required review blocks every auto-merge.

   ```bash
   gh api -X PUT repos/yama6a/pi5-k8s-sample-app/branches/main/protection \
     -H "Accept: application/vnd.github+json" --input - <<'JSON'
   {
     "required_status_checks": { "strict": false, "checks": [{"context": "go / go"}, {"context": "renovate-config"}] },
     "enforce_admins": false,
     "required_pull_request_reviews": null,
     "restrictions": null
   }
   JSON
   ```
