package merkle

type Tree struct{}

func New(values [][]byte) *Tree {
	return &Tree{}
}

func (t *Tree) Root() []byte {
	return nil
}

func (t *Tree) Validate(values [][]byte) []int {
	return nil
}
