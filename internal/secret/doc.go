// Package secret keeps profile passwords and key passphrases in the OS
// credential store (mygo.Secrets), with an in-memory session fallback when
// the store is unavailable. Secrets never reach the database.
package secret
