# cairn — convenience targets.

.PHONY: test-skills-verify test-skills-record

# Static checks over committed skill fixtures. CI calls this on Linux.
# See Ship 3 design §8.2.
test-skills-verify:
	go run ./testdata/skill-tests/verify/

# Human-triggered fixture regeneration. Not CI-gated. Outputs step-by-step
# instructions; the agent invocation against the prose inputs is manual.
# See Ship 3 design §8.2 (test-skills-record bullet).
test-skills-record:
	@echo "==== test-skills-record ===="
	@echo "This target documents the manual fixture regeneration procedure."
	@echo ""
	@echo "1. Open Claude Code with cairn + superpowers installed."
	@echo "2. For each fixture directory under testdata/skill-tests/yaml-authoring/:"
	@echo "   a. Read the prose input (design.md or design-before.md)."
	@echo "   b. Invoke the using-cairn skill against the prose."
	@echo "   c. Compare derived YAML output to the committed fixture."
	@echo "   d. If output differs, overwrite the fixture file with new output."
	@echo "3. Re-run 'make test-skills-verify' to confirm consistency."
	@echo "4. Commit fixture changes in the same PR as the skill change."
	@echo ""
	@echo "Human diligence required: the static checker cannot detect a"
	@echo "skill that produces output matching the fixture shape but with"
	@echo "different content. See design §8.2 'Limitation acknowledged'."
