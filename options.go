package deterbus

// BusOption is an optional parameter for Bus construction.
type BusOption func(opt *busOptions)

// BusOptions is a built set of Bus construction parameters.
type busOptions struct {
}

func buildOptions(opts ...BusOption) busOptions {
	options := &busOptions{}
	// apply opts, if any
	for _, opt := range opts {
		opt(options)
	}
	return *options
}
