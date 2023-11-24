package deterbus

// BusOption is an optional parameter for Bus construction.
type BusOption func(opt *busOptions)

// BusOptions is a built set of Bus construction parameters.
type busOptions struct {
	validate bool
}

func buildOptions(opts ...BusOption) busOptions {
	options := &busOptions{
		// default values go in here
		validate: true,
	}
	// apply opts, if any
	for _, opt := range opts {
		opt(options)
	}
	return *options
}

// DontValidate skips reflection based type checks on publish and subscribe.
func DontValidate(opt *busOptions) {
	opt.validate = false
}
