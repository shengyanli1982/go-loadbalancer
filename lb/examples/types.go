package examples

type simpleBackend struct {
	addr string
}

func (b *simpleBackend) Address() string {
	return b.addr
}

type simpleWeightedBackend struct {
	addr   string
	weight int
}

func (b *simpleWeightedBackend) Address() string {
	return b.addr
}

func (b *simpleWeightedBackend) Weight() int {
	return b.weight
}
