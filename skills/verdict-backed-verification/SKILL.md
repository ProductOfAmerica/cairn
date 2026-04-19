---
name: verdict-backed-verification
description: Use when about to claim work is complete inside an active cairn claim. Composition over superpowers:verification-before-completion — same Iron Law, with evidence put + verdict bound before the completion claim. Tracked-only. Without an active claim, use superpowers:verification-before-completion directly.
---

## 1. Preamble (hard boundary)

> Invoke this skill ONLY while holding a cairn claim. If you are not inside a cairn-tracked task, use `superpowers:verification-before-completion` directly. This skill will error if no active claim is in scope.

## 2. Gate-function delta over SP V-B-C

| SP V-B-C step | Cairn addition | Command |
|---|---|---|
| 1. IDENTIFY (what command proves this claim?) | no change | — |
| 2. RUN (execute full command) | capture stdout+stderr to file | `<cmd> > /tmp/gate-output.txt 2>&1` |
| 3. READ (full output, exit code, failures) | store as evidence | `cairn evidence put /tmp/gate-output.txt` |
| 4. VERIFY (does output confirm claim?) — on PASS | bind verdict | `cairn verdict report --gate <id> --run <run_id> --status pass --evidence /tmp/gate-output.txt --producer-hash <placeholder> --inputs-hash <placeholder>` |
| 4. VERIFY — on FAIL | bind fail verdict, don't claim complete | `cairn verdict report ... --status fail ...`; return to SP step 1 |

## 3. Claim wording — machine-readable JSON blob

Step 5 CLAIM emits a JSON blob on stdout, terminated by newline. Orchestrators parse; humans read the JSON directly (keys are self-documenting).

```json
{"verdict_id":"VDCT_01H...","evidence_sha256":"9f3c...","status":"pass","gate_id":"AC-001","run_id":"RUN_01H..."}
```

Format rules: compact JSON (no pretty-print), single line, keys in the order shown. Parser hint: the line begins `{"verdict_id":` — orchestrators can grep for this prefix to locate the claim.

Prose status text ("verdict bound pass, evidence stored") is permitted before or after the JSON blob for human readability but MUST NOT replace the JSON. The JSON line is the load-bearing artifact.

## 4. Iron Law (extended)

> NO COMPLETION CLAIMS WITHOUT FRESH VERIFICATION EVIDENCE **BOUND AS A CAIRN VERDICT**.

## 5. Red Flags

| Thought | Reality |
|---|---|
| "Skip the evidence put, the output was already captured" | NEVER. The verdict binding requires a cairn-stored evidence row. |
| "Bind the verdict to a different run id to reuse evidence" | NEVER. Run id must match the active claim's run. |
