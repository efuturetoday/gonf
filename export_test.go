package gonf

// LiveDone returns a channel that is closed when watching has stopped.
func LiveDone(l *Live) <-chan struct{} { return l.done }

// BindMapping binds m to the values of c the way Load binds its Mappings,
// on a copy of c, and returns the bound value. Tests use it to bind many
// structs to one loaded Config.
func BindMapping[T any](c *Config, m *Mapping[T]) (T, error) {
	cp := *c
	if err := cp.bindMappings([]AnyMapping{m}); err != nil {
		var zero T
		return zero, err
	}
	return m.Get(&cp), nil
}
