# Agent Runtime Rules

- Make the minimal correct change for the bead.
- Do not modify unrelated files.
- Do not push branches or create PRs (the outer script handles that).
- If tests fail, fix the code unless tests are clearly wrong.
- Focus on making ./scripts/ci.sh pass.
- Stop after 3 repeated failures on the same error and report.
- If you add/modify tests, change contract/policy behavior, or touch goldens/fixtures, follow test-excellence (RED → GREEN → PROVE) and report evidence.
- If a bead changes contract/policy behavior and lacks coverage, add/extend the relevant contract test (see docs/Contract-Test-Checklist.md).
- Use golden-defaults only when scripts/ci.sh is missing or CI/tooling scope is unclear; otherwise scripts/ci.sh is the gate.
