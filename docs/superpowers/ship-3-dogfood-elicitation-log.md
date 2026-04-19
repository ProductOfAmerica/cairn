# Ship 3 Dogfood — C1 Forcing Test Elicitation Log

> Status: **gate-pending-human-session**
>
> The C1 forcing test (design §6.3) requires a fresh interactive Claude Code
> session with the cairn plugin installed and the three Ship 3 skills active.
> This session was an autonomous build session — no fresh human session was
> available. The forcing test is deferred until the user runs it manually
> before merging Ship 3.

## Protocol (for the user to execute)

1. Open a fresh Claude Code session in the cairn repo with the cairn plugin
   active (all three Ship 3 skills loaded: `using-cairn`,
   `subagent-driven-development-with-verdicts`,
   `verdict-backed-verification`).

2. Pick a hypothetical feature for a fictional project (not a real cairn
   feature — this is a throwaway test). Example: "Let's design a
   user-notification batching system for the external-api service."

3. Tell the main session: `superpowers:brainstorming` scoped to
   `testdata/forcing-test/`. Let brainstorming run to design approval and
   commit the prose into `testdata/forcing-test/design.md`.

4. After design approval, explicitly invoke `using-cairn` in the same
   session.

5. Let `using-cairn` run the elicitation flow. **As the human, record every
   question the agent poses** verbatim below, in the format defined in
   design §6.3:

   ```
   ## <timestamp> — REQ-NNN: <title>
   Q1 (distinct design decision — <what decision>): <verbatim question>
   Q2 (clarification on Q1 — counts as 0): <verbatim question>
   Q3 (distinct design decision — <what decision>): <verbatim question>
   ...
   Total distinct design decisions: <N>
   ```

6. Apply pass/fail criterion per design §6.3:
   - **Pass** — all requirements have ≤ 3 distinct design decisions.
   - **Fail** — any requirement exceeds 3 → open amendment to swap D→B
     and re-run before merging Ship 3.

7. Commit the derived YAML artifacts (requirement + task files) under
   `testdata/forcing-test/specs/` and update this log with the
   elicitation records.

## Results

*To be filled in by the user after running the forcing test.*

| REQ | Distinct decisions | Pass? |
|-----|--------------------|-------|
| (pending human session) | — | — |
