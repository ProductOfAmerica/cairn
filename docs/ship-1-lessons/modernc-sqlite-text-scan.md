# Lesson: modernc.org/sqlite returns TEXT as `string`, not `[]byte`

**One-line summary:** When scanning a SQLite TEXT column via
`modernc.org/sqlite`, always scan into a `string` intermediate and then
convert to `[]byte` / `json.RawMessage`. Direct scan into a `[]byte`-family
target fails at runtime.

## Failure mode

```
sql: Scan error on column index N, name "X":
  unsupported Scan, storing driver.Value type string into type *json.RawMessage
```

## Correct pattern

```go
var payloadStr string
if err := rows.Scan(..., &payloadStr, ...); err != nil {
    return err
}
e.Payload = json.RawMessage(payloadStr)
```

## Incorrect (fails at runtime)

```go
var e Event // Payload is json.RawMessage
if err := rows.Scan(..., &e.Payload, ...); err != nil {
    return err
}
```

## Why

`modernc.org/sqlite` registers `string` as the native Go type for TEXT
columns at the `driver.Value` level. `database/sql`'s `Scan` implementation
does not know how to convert `string` into `*[]byte` or `*json.RawMessage`,
so it returns the error above.

## How this surfaced during Ship 1

Discovered during Task 6.1 of cairn Ship 1 (2026-04-17). A reviewer
suggested removing the `payloadStr` intermediate as "redundant", claiming
the driver returns TEXT as `[]byte`. The direct scan landed, tests failed,
intermediate was restored.

## How to apply (for Ship 2 and beyond)

- Keep the `string` intermediate when scanning TEXT columns into `[]byte`
  family types. Add a short comment on the intermediate explaining why so
  future well-meaning refactors don't repeat the mistake.
- This applies to `modernc.org/sqlite`; other drivers (CGO
  `mattn/go-sqlite3`) may handle this differently. If the driver changes,
  re-verify.
- Reviewer advice about driver internals deserves verification. Do not
  accept "this works because the driver does X" unless the reviewer
  demonstrates it with a passing test.
