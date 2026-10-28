// Package history stores finished sessions as JSON Lines (history/<YYYY-MM>.jsonl),
// the output and events of each session (sessions/<id>.log, sessions/<id>.events.jsonl)
// and the backups of changed files (backups/<id>/<path>), and applies the retention
// settings history.keepDays and history.keepSessions (docs/concept.md, "Dateien und
// Formate").
package history
