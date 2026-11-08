// Package parser provides immutable text and independent, stateful cursors for
// line-oriented, multi-pass parsing. Reading never marks a line as parsed;
// callers record successful parsing explicitly with [Cursor.SetParsed].
//
// Content preserves source bytes, including line endings and invalid UTF-8.
// Syntax, character decoding and device metadata belong to callers.
package parser
