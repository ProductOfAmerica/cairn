# Hash Placeholders

## Banner

> These hashes are placeholders. They do not reflect toolchain version or input state. Verdicts bound with these values are NOT safe to rely on for cross-run drift detection.

## Recipe

```
producer_hash = sha256("ship3:" + gate.id + ":" + gate.producer.kind)
inputs_hash   = sha256("ship3:" + run_id)
```

Shell computation:

```bash
producer_hash=$(printf 'ship3:%s:%s' "$gate_id" "$producer_kind" \
    | sha256sum | cut -d' ' -f1)
inputs_hash=$(printf 'ship3:%s' "$run_id" \
    | sha256sum | cut -d' ' -f1)
```

## Forbidden Uses

- MUST NOT be used as a staleness signal.
- MUST NOT be compared across runs as evidence of input change.
- MUST NOT be presented to humans as toolchain version.

## Replacement Plan

Ship 3 post-dogfood check records a binary flag: "did anyone misread provisional hashes as meaningful?" Stored in `docs/superpowers/ship-3-dogfood-summary.md`.

- If yes → accelerate Q1 of `docs/superpowers/ship-3-open-questions.md` to Ship 4 week 1.
- If no → Q1 stays queued until a concrete use case forces it.
