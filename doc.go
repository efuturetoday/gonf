// Package gonf is a layered configuration library modeled on
// SmallRye/MicroProfile Config.
//
// Configuration is read from several Sources. Each Source has an ordinal;
// for every key the value from the Source with the highest ordinal wins.
// If two Sources have the same ordinal, the one registered later wins.
//
// A key prefixed with "%profile." such as "%dev.db.url" is a value for
// "db.url" that applies only while that profile is active (see
// WithProfiles). At equal ordinal a profile value beats the base value; a
// higher ordinal always wins, so an environment variable still overrides a
// profile value from a file.
//
// Values may reference other keys with ${key} or ${key:default}; Get expands
// them.
//
// Sources that need configuration themselves, such as a Vault Source whose
// address is configured in a file, are created by a SourceFactory (see
// WithFactories).
package gonf
