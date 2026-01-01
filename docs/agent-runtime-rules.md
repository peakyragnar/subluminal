# Agent Runtime Rules

- Make the minimal correct change for the bead.
- Do not modify unrelated files.
- Do not push branches or create PRs (the outer script handles that).
- If tests fail, fix the code unless tests are clearly wrong.
- Focus on making ./scripts/ci.sh pass.
- Stop after 3 repeated failures on the same error and report.
