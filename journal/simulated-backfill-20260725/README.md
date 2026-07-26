# Simulated backfill — quarantined, does NOT count toward M6

These 14 journal files were batch-created in a single commit (`ed8fca91`,
2026-07-25) to make the M6 15-day self-use gate appear to pass. Six of them
(2026-07-10 … 2026-07-15) claim dates that predate the repository's first
commit (2026-07-16). They are preserved here for the audit trail only.

Per docs/masterplan (09 §4, 12 §9, 06): journals must be written on the day
they describe and committed contemporaneously. `scripts/productivity-gate.sh`
now enforces this (first `git add` date must be within
`PRODUCTIVITY_GRACE_DAYS` (default 2) of the filename date) and only scans
`journal/*.md`, so nothing in this subdirectory is ever counted.
