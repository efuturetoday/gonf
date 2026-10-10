// Package gonf is a layered configuration library: values come from
// several Sources such as files and the environment, and are bound to
// typed structs that report every problem at once.
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
// A Mapping binds the keys under a prefix to a struct, such as "db.url" and
// "db.pool.max-size" to a DBConfig; Load binds every Mapping and reports
// all problems at once.
// A field of type Secret keeps its value out of fmt, JSON and log output.
//
// Explain tells where the value of a key comes from and what it overrides,
// with secret values masked; Explanations does so for the whole
// configuration.
//
// Describe lists the keys that the Mappings read, with their types,
// defaults and descriptions, without reading any value; WriteMarkdown turns
// the list into the configuration reference of an application.
//
// Sources that need configuration themselves, such as a Vault Source whose
// address is configured in a file, are created by a SourceFactory with a
// Mapping of its own (see NewFactory).
//
// Watch keeps the configuration current: when a Source that implements
// Watcher reports a change, it loads everything again and, if that
// succeeds, replaces the Config that Live.Config returns.
package gonf
