# parser

Follow [the gatework Go engineering standard](../go-standards/STANDARD.md) (go-standards/STANDARD.md); this file adds only what is specific to parser.

Use `golang-how-to` for Go work and select the relevant secondary skills.

Before changing public behavior, read README.md for line, cursor and ownership
contracts. Keep the library independent of firewall models and vendor syntax.
Use the standard library and verify public behavior through external-package
tests. Treat source bytes and unparsed lines as lossless data.

Run `make check` before every commit, and `make fuzz` as well for changes to indexing or cursor semantics.
