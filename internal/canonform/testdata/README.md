# canonform fixtures

Pairs of equivalent JSON inputs (different key orderings, varying whitespace) and the expected canonical-form bytes for each. Used by `canonform_test.go` (T007).

Files:
- `input_keys_reordered.json` — keys in non-sorted order, with whitespace
- `input_keys_sorted_compact.json` — same logical content, keys sorted, no insignificant whitespace
- `expected_canonical.bin` — the canonical-form bytes both inputs must produce

The `Marshal` function MUST produce `expected_canonical.bin` byte-for-byte from either input (after `json.Unmarshal` to a generic `any`).
