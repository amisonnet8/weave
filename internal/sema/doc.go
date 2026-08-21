// Package sema performs Weave's semantic checks (scope resolution and
// syntax-level validation) ahead of code generation. Per CLAUDE.md's
// "意味検証の責任分担" section, Weave's dynamic typing means most
// value-type errors surface at runtime rather than here.
package sema
